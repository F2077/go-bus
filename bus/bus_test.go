package bus

import (
	"bytes"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// safeBuffer 是并发安全的字节缓冲，专用于跨 goroutine 捕获 slog 输出。
// listener goroutine 经 recover→logger.Error 写入，主 goroutine 读取断言，
// 二者须共持一锁以建立 happens-before，否则 race detector 必报 data race。
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// newTestBus 构造一个 Bus：构造失败即 require 失败，并注册 Close 于测试/基准
// 结束（goleak）。Test 与 Benchmark 共用（testing.TB 兼容二者）。
func newTestBus(tb testing.TB, opts ...BusOption) *Bus {
	tb.Helper()
	bus, err := NewBus(opts...)
	require.NoError(tb, err)
	tb.Cleanup(bus.Close)
	return bus
}

func TestBus_OnAndEmit(t *testing.T) {
	bus := newTestBus(t)

	var wg sync.WaitGroup
	wg.Add(1)

	listener := NewListener(func(msg any) {
		defer wg.Done()
		assert.Equal(t, "test payload", msg)
	})

	require.NoError(t, bus.On(NewEvent(1), listener))
	require.NoError(t, bus.Emit(NewEvent(1), "test payload"))

	wg.Wait()
}

func TestListenerCancel(t *testing.T) {
	bus := newTestBus(t)

	var called atomic.Bool
	listener := NewListener(func(msg any) {
		called.Store(true)
	})

	require.NoError(t, bus.On(NewEvent(2), listener))

	listener.Cancel()
	require.NoError(t, bus.Emit(NewEvent(2), "data"))

	time.Sleep(100 * time.Millisecond) // 等待 goroutine 退出
	assert.False(t, called.Load())
}

// TestPanicRecovery 验证回调 panic 后监听器继续运行（非退出）：第二条消息仍被处理。
func TestPanicRecovery(t *testing.T) {
	var logBuffer safeBuffer
	bus := newTestBus(t, WithLogger(slog.New(slog.NewJSONHandler(&logBuffer, nil))))

	var calls atomic.Int32
	listener := NewListener(func(msg any) {
		if calls.Add(1) == 1 {
			panic("simulated panic")
		}
	})

	require.NoError(t, bus.On(NewEvent(3), listener))
	_ = bus.Emit(NewEvent(3), nil) // 触发 panic
	_ = bus.Emit(NewEvent(3), nil) // panic 后应继续接收

	assert.Eventually(t, func() bool {
		return calls.Load() >= 2 // panic 后 listener 仍处理第二条
	}, time.Second, 5*time.Millisecond, "listener should keep running after a recovered panic")
	assert.Contains(t, logBuffer.String(), "listener panic recovered")
}

func TestConcurrentAccess(t *testing.T) {
	bus := newTestBus(t)

	const numListeners = 100
	var counter atomic.Int64

	var setupWG sync.WaitGroup
	setupWG.Add(numListeners)
	for i := 0; i < numListeners; i++ {
		l := NewListener(func(msg any) {
			counter.Add(1)
		})
		go func() {
			defer setupWG.Done()
			_ = bus.On(NewEvent(4), l)
		}()
	}
	setupWG.Wait()

	var emitWG sync.WaitGroup
	for i := 0; i < 10; i++ {
		emitWG.Add(1)
		go func() {
			defer emitWG.Done()
			_ = bus.Emit(NewEvent(4), "data")
		}()
	}
	emitWG.Wait()

	assert.Eventually(t, func() bool {
		return counter.Load() == int64(10*numListeners)
	}, time.Second, 5*time.Millisecond, "Expected %d but got %d", 10*numListeners, counter.Load())
}

func TestEventTypeMismatch(t *testing.T) {
	bus := newTestBus(t)

	var called atomic.Bool
	listener := NewListener(func(msg any) {
		called.Store(true)
	})

	require.NoError(t, bus.On(NewEvent(5), listener))
	_ = bus.Emit(NewEvent(6), "data")

	time.Sleep(100 * time.Millisecond)
	assert.False(t, called.Load())
}

func TestOneTimeListener(t *testing.T) {
	bus := newTestBus(t)

	var calls atomic.Int32
	listener := NewListener(func(msg any) {
		calls.Add(1)
	}, WithOnetime(true))
	require.NoError(t, bus.On(NewEvent(7), listener))

	for i := 0; i < 5; i++ {
		_ = bus.Emit(NewEvent(7), "data")
	}

	assert.Eventually(t, func() bool {
		return calls.Load() == 1
	}, time.Second, 5*time.Millisecond,
		"one-time listener should fire exactly once, got %d", calls.Load())
}

func TestEmitNoListeners(t *testing.T) {
	bus := newTestBus(t)

	// 无任何监听器时，Emit 应静默成功，不 panic
	assert.NotPanics(t, func() {
		assert.NoError(t, bus.Emit(NewEvent(999), "orphan"))
	})
}

func TestMultipleListenersBroadcast(t *testing.T) {
	bus := newTestBus(t)

	const n = 5
	var got [n]atomic.Int32
	for i := 0; i < n; i++ {
		idx := i
		require.NoError(t, bus.On(NewEvent(8), NewListener(func(msg any) {
			got[idx].Add(1)
		})))
	}

	require.NoError(t, bus.Emit(NewEvent(8), "broadcast"))

	// 一次 Emit 应广播至全部 n 个监听器
	assert.Eventually(t, func() bool {
		for i := 0; i < n; i++ {
			if got[i].Load() != 1 {
				return false
			}
		}
		return true
	}, time.Second, 5*time.Millisecond, "expected all %d listeners to receive once", n)
}

func TestNewBusWithNilLogger(t *testing.T) {
	// WithLogger(nil) 使底层 broker 构造失败（go-pubsub 的 ErrLoggerNil），NewBus 应返回错误
	_, err := NewBus(WithLogger(nil))
	assert.Error(t, err)
}

func TestNilOptionsIgnored(t *testing.T) {
	// nil 选项应被静默跳过，不影响构造
	assert.NotPanics(t, func() {
		b, err := NewBus(nil, nil)
		require.NoError(t, err)
		require.NotNil(t, b)

		l := NewListener(func(msg any) {}, nil, nil)
		require.NotNil(t, l)
	})
}

// TestCapacityExceeded 验证 WithCapacity 之 topic 上限：超出后 Emit 返回错误。
func TestCapacityExceeded(t *testing.T) {
	bus := newTestBus(t, WithCapacity(4))

	for i := 0; i < 4; i++ {
		require.NoError(t, bus.Emit(NewEvent(100+i), "ok"))
	}
	err := bus.Emit(NewEvent(200), "overflow")
	assert.Error(t, err)
}

func TestCloseCancelsListeners(t *testing.T) {
	bus := newTestBus(t)

	var called atomic.Int32
	require.NoError(t, bus.On(NewEvent(1), NewListener(func(msg any) {
		called.Add(1)
	})))

	bus.Close() // 取消所有已注册监听器

	_ = bus.Emit(NewEvent(1), "after-close")
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load(), "Close should stop all listeners")
}

// BenchmarkEventBus 度量 1000 监听器下并发 Emit 的吞吐。
func BenchmarkEventBus(b *testing.B) {
	bus := newTestBus(b)

	const listeners = 1000
	var ready sync.WaitGroup
	ready.Add(listeners)
	for i := 0; i < listeners; i++ {
		l := NewListener(func(msg any) {
			atomic.AddInt64(&msgCount, 1)
		})
		go func(e Event) {
			defer ready.Done()
			_ = bus.On(e, l)
		}(NewEvent(i))
	}
	ready.Wait()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.Emit(NewEvent(0), "payload")
		}
	})
}

// BenchmarkFanout 度量单次 Emit 广播至 N 个监听器的延迟，随 N 递增。
func BenchmarkFanout(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus := newTestBus(b)
			for i := 0; i < n; i++ {
				_ = bus.On(NewEvent(1), NewListener(func(msg any) {}))
			}
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_ = bus.Emit(NewEvent(1), "payload")
				}
			})
		})
	}
}

// BenchmarkOneTimeListener 度量 onetime 监听器的 On+Emit+自动退出全路径。
func BenchmarkOneTimeListener(b *testing.B) {
	bus := newTestBus(b)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.On(NewEvent(1), NewListener(func(msg any) {}, WithOnetime(true)))
			_ = bus.Emit(NewEvent(1), "payload")
		}
	})
}

var msgCount int64

// BenchmarkMemoryAllocations 度量每次 On+Emit+注销的分配。每轮独立 listener
// 并 Cancel，令 goroutine 即时退出；topic 取 i%64 限制 broker 状态增长，
// 使每次迭代成本稳定 O(1)，而非随 b.N 线性膨胀。
func BenchmarkMemoryAllocations(b *testing.B) {
	bus := newTestBus(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := NewListener(func(msg any) {})
		_ = bus.On(NewEvent(i%64), l)
		_ = bus.Emit(NewEvent(i%64), "payload")
		l.Cancel()
	}
}
