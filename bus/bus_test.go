package bus

import (
	"bytes"
	"fmt"
	"log/slog"
	"runtime"
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

func TestBus_OnAndEmit(t *testing.T) {
	bus, _ := NewBus()
	var wg sync.WaitGroup
	wg.Add(1)

	listener := NewListener(func(msg any) {
		defer wg.Done()
		assert.Equal(t, "test payload", msg)
	})

	err := bus.On(NewEvent(1), listener)
	assert.NoError(t, err)

	err = bus.Emit(NewEvent(1), "test payload")
	assert.NoError(t, err)

	wg.Wait()
}

func TestListenerCancel(t *testing.T) {
	bus, _ := NewBus()
	var called atomic.Bool

	listener := NewListener(func(msg any) {
		called.Store(true)
	})

	err := bus.On(NewEvent(2), listener)
	assert.NoError(t, err)

	listener.Cancel()
	err = bus.Emit(NewEvent(2), "data")
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond) // 等待goroutine退出
	assert.False(t, called.Load())
}

func TestPanicRecovery(t *testing.T) {
	var logBuffer safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))

	bus, _ := NewBus(WithLogger(logger))
	listener := NewListener(func(msg any) {
		panic("simulated panic")
	})

	err := bus.On(NewEvent(3), listener)
	assert.NoError(t, err)

	_ = bus.Emit(NewEvent(3), nil)
	time.Sleep(100 * time.Millisecond)

	assert.Contains(t, logBuffer.String(), "listener panic recovered")
}

func TestConcurrentAccess(t *testing.T) {
	bus, _ := NewBus()
	const numListeners = 100
	var counter int64 // Change to atomic int

	// Use wait group to ensure all listeners are registered
	var setupWG sync.WaitGroup
	setupWG.Add(numListeners)

	for i := 0; i < numListeners; i++ {
		listener := NewListener(func(msg any) {
			atomic.AddInt64(&counter, 1)
		})
		go func() {
			_ = bus.On(NewEvent(4), listener)
			setupWG.Done()
		}()
	}
	setupWG.Wait()

	// Use separate wait group for emissions
	var emitWG sync.WaitGroup
	for i := 0; i < 10; i++ {
		emitWG.Add(1)
		go func() {
			defer emitWG.Done()
			_ = bus.Emit(NewEvent(4), "data")
		}()
	}
	emitWG.Wait()

	// Verify counter with retries
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&counter) == int64(10*numListeners)
	}, 1*time.Second, 100*time.Millisecond, "Expected %d but got %d", 10*numListeners, atomic.LoadInt64(&counter))
}

func TestEventTypeMismatch(t *testing.T) {
	bus, _ := NewBus()
	called := false

	listener := NewListener(func(msg any) {
		called = true
	})

	_ = bus.On(NewEvent(5), listener)
	_ = bus.Emit(NewEvent(6), "data")

	time.Sleep(100 * time.Millisecond)
	assert.False(t, called)
}

func TestOneTimeListener(t *testing.T) {
	bus, _ := NewBus()
	var calls atomic.Int32

	// WithOnetime(true) 的监听器触发一次后自动停止，后续 Emit 不再触发
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
	bus, _ := NewBus()
	// 无任何监听器时，Emit 应静默成功，不 panic
	assert.NotPanics(t, func() {
		assert.NoError(t, bus.Emit(NewEvent(999), "orphan"))
	})
}

func TestMultipleListenersBroadcast(t *testing.T) {
	bus, _ := NewBus()
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
	// WithLogger(nil) 使底层 broker 构造失败（go-pubsub v1.1+ 的 ErrLoggerNil），NewBus 应返回错误
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

func BenchmarkEventBus(b *testing.B) {
	bus, _ := NewBus()
	const listeners = 1000
	var ready sync.WaitGroup
	ready.Add(listeners)

	// 预注册监听器
	for i := 0; i < listeners; i++ {
		listener := NewListener(func(msg any) {
			atomic.AddInt64(&msgCount, 1)
		})
		go func(e Event) {
			_ = bus.On(e, listener)
			ready.Done()
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

func TestStressWithGoroutineLeakCheck(t *testing.T) {
	// Add brief sleep to let runtime settle
	runtime.Gosched()
	initialGoroutines := runtime.NumGoroutine()

	t.Run("100k_events", func(t *testing.T) {
		bus, _ := NewBus()
		const (
			listeners    = 500
			eventsPerSec = 10000
		)

		var wg sync.WaitGroup
		wg.Add(listeners)

		// Store listeners for cleanup
		listenerPool := make([]*Listener, listeners)

		// 注册监听器
		for i := 0; i < listeners; i++ {
			listener := NewListener(func(msg any) {
				atomic.AddInt64(&eventCounter, 1)
			})
			listenerPool[i] = listener

			go func(e Event) {
				defer wg.Done()
				_ = bus.On(e, listener)
			}(NewEvent(i % 10))
		}
		wg.Wait()

		// 压力测试阶段
		start := time.Now()
		var emitWg sync.WaitGroup
		for i := 0; i < eventsPerSec; i++ {
			emitWg.Add(1)
			go func(e Event) {
				defer emitWg.Done()
				_ = bus.Emit(e, "stress_test")
			}(NewEvent(i % 10))
		}
		emitWg.Wait()

		// Add cleanup phase
		t.Cleanup(func() {
			for _, l := range listenerPool {
				l.Cancel()
			}
			time.Sleep(100 * time.Millisecond) // Allow goroutines to exit
		})

		t.Logf("Processed %d events in %v", eventsPerSec, time.Since(start))
	})

	// Add final GC to clean up
	runtime.GC()

	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= initialGoroutines+5 // Increase buffer
	}, 3*time.Second, 300*time.Millisecond,
		"goroutine leak detected, before: %d, after: %d",
		initialGoroutines, runtime.NumGoroutine())
}

var (
	msgCount     int64
	eventCounter int64
)

// 内存分配分析测试
func BenchmarkMemoryAllocations(b *testing.B) {
	bus, _ := NewBus()
	listener := NewListener(func(msg any) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.On(NewEvent(i), listener)
		_ = bus.Emit(NewEvent(i), "payload")
	}
}

// BenchmarkFanout 度量单次 Emit 广播至 N 个监听器的延迟，随 N 递增。
// 此乃事件总线之核心场景：fan-out 吞吐与分配。
func BenchmarkFanout(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus, _ := NewBus()
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
	bus, _ := NewBus()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.On(NewEvent(1), NewListener(func(msg any) {}, WithOnetime(true)))
			_ = bus.Emit(NewEvent(1), "payload")
		}
	})
}
