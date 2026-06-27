package bus

import (
	"context"
	"github.com/F2077/go-pubsub/pubsub"
	"log/slog"
	"os"
	"strconv"
	"sync"
)

type Event int

func (e Event) String() string {
	return strconv.Itoa(int(e))
}

func NewEvent(v int) Event {
	return Event(v)
}

type Bus struct {
	logger    *slog.Logger
	broker    *pubsub.Broker[message]
	publisher *pubsub.Publisher[message] // 复用：省 Emit 时每次新建 publisher 的 uuid 分配
	capacity  uint32

	mu        sync.Mutex
	listeners map[*Listener]struct{} // 已注册监听器；Close 统一取消
}

type BusOption func(*Bus)

// WithLogger injects an slog.Logger; recovered listener panics are reported
// through it. The default is a quiet text handler at Error level writing to
// stdout.
func WithLogger(logger *slog.Logger) BusOption {
	return func(bus *Bus) {
		bus.logger = logger
	}
}

// WithCapacity sets the maximum number of distinct Event values (topics) the
// broker holds before Emit returns a wrapped ErrSubscriptionCapacityExceeded.
// The default matches pubsub.DefaultCapacity (8192). Raise it only if the bus
// fans out across many distinct events; the intended usage is a small set of
// semantic Event constants with high-cardinality data carried in the payload,
// not as event values.
func WithCapacity(capacity uint32) BusOption {
	return func(bus *Bus) {
		bus.capacity = capacity
	}
}

func NewBus(option ...BusOption) (*Bus, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelError,
	}))
	b := &Bus{
		logger:    logger,
		capacity:  pubsub.DefaultCapacity,
		listeners: make(map[*Listener]struct{}),
	}
	for _, opt := range option {
		if opt == nil {
			continue
		}
		opt(b)
	}
	broker, err := pubsub.NewBroker[message](
		pubsub.WithLogger[message](b.logger),
		pubsub.WithCapacity[message](b.capacity),
	)
	if err != nil {
		return nil, err
	}
	b.broker = broker
	b.publisher = pubsub.NewPublisher(broker)
	return b, nil
}

// On registers a listener on event, starting a goroutine that runs the
// callback for every delivered message until the listener is canceled, becomes
// one-shot and fires, or Close is called. A panic in the callback is recovered
// and logged; the listener keeps running on subsequent messages.
func (b *Bus) On(event Event, listener *Listener) error {
	// 每次 On 新建一个 subscriber：go-pubsub 在同一 subscriber 上对同一 topic
	// 复用同一条 channel，若复用则同 topic 的多个 listener 会共享一条 channel、
	// 相互抢消息，破坏 fan-out 语义。故每 listener 须独立 subscriber（独立 channel）。
	// 这与 publisher 之复用不同：publisher 无状态，subscriber 持有与订阅身份绑定的
	// channels 映射。详见 bus_test.go TestMultipleListenersBroadcast。
	subscriber := pubsub.NewSubscriber[message](b.broker)
	sub, err := subscriber.Subscribe(event.String())
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.listeners[listener] = struct{}{}
	b.mu.Unlock()

	go func() {
		defer func() {
			_ = sub.Close()
			b.mu.Lock()
			delete(b.listeners, listener)
			b.mu.Unlock()
		}()
		for {
			select {
			case <-listener.ctx.Done():
				return
			case m, ok := <-sub.Ch:
				if !ok {
					return
				}
				if listener.ctx.Err() != nil {
					// Cancel 与消费之间的竞态：已出队但未处理的消息丢弃，使
					// Cancel 立即生效。one-shot listener 在此情形下可能零触发。
					return
				}
				b.runCallback(listener, m.payload)
				if listener.onetime {
					return
				}
			}
		}
	}()
	return nil
}

// runCallback runs the listener callback with panic isolation: a panicking
// callback is recovered and logged, and the caller's loop continues with the
// next message (the listener is not killed).
func (b *Bus) runCallback(listener *Listener, payload any) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("listener panic recovered", "error", r)
		}
	}()
	listener.listenFunc(payload)
}

func (b *Bus) Emit(event Event, msg any) error {
	return b.publisher.Publish(event.String(), message{
		event:   event,
		payload: msg,
	})
}

// Close cancels every listener registered via On, stopping all listener
// goroutines. It is safe to call more than once. Listeners canceled
// individually via Listener.Cancel need not be closed again.
func (b *Bus) Close() {
	b.mu.Lock()
	listeners := make([]*Listener, 0, len(b.listeners))
	for l := range b.listeners {
		listeners = append(listeners, l)
	}
	b.mu.Unlock()
	for _, l := range listeners {
		l.Cancel()
	}
}

type Listener struct {
	ctx        context.Context
	cancel     context.CancelFunc
	listenFunc ListenFunc
	onetime    bool
}

type ListenerOption func(*Listener)

func WithOnetime(onetime bool) ListenerOption {
	return func(l *Listener) {
		l.onetime = onetime
	}
}

func NewListener(f ListenFunc, opts ...ListenerOption) *Listener {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		ctx:        ctx,
		cancel:     cancel,
		listenFunc: f,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

func (l *Listener) Cancel() {
	l.cancel()
}

type ListenFunc func(msg any)

type message struct {
	event   Event
	payload any
}
