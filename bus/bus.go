package bus

import (
	"context"
	"github.com/F2077/go-pubsub/pubsub"
	"log/slog"
	"os"
	"strconv"
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
}

type BusOption func(*Bus)

func WithLogger(logger *slog.Logger) BusOption {
	return func(bus *Bus) {
		bus.logger = logger
	}
}

func NewBus(option ...BusOption) (*Bus, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelError,
	}))
	b := &Bus{
		logger: logger,
	}
	for _, opt := range option {
		if opt == nil {
			continue
		}
		opt(b)
	}
	broker, err := pubsub.NewBroker[message](pubsub.WithLogger[message](b.logger))
	if err != nil {
		return nil, err
	}
	b.broker = broker
	b.publisher = pubsub.NewPublisher(broker)
	return b, nil
}

func (b *Bus) On(event Event, listener *Listener) error {
	subscriber := pubsub.NewSubscriber[message](b.broker)
	sub, err := subscriber.Subscribe(event.String())
	if err != nil {
		return err
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("listener panic recovered", "error", r)
			}
		}()
		defer func(sub *pubsub.Subscription[message]) {
			_ = sub.Close()
		}(sub)
		for {
			select {
			case <-listener.ctx.Done():
				return
			case m, ok := <-sub.Ch:
				if !ok {
					return
				}
				if listener.ctx.Err() != nil {
					return // Cancel 后不再处理已投递消息
				}
				listener.listenFunc(m.payload)
				if listener.onetime {
					return
				}
			}
		}
	}()
	return nil
}

func (b *Bus) Emit(event Event, msg any) error {
	return b.publisher.Publish(event.String(), message{
		event:   event,
		payload: msg,
	})
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
