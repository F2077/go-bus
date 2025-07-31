package bus

import (
	"bytes"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
	called := false

	listener := NewListener(func(msg any) {
		called = true
	})

	err := bus.On(NewEvent(2), listener)
	assert.NoError(t, err)

	listener.Cancel()
	err = bus.Emit(NewEvent(2), "data")
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond) // 等待goroutine退出
	assert.False(t, called)
}

func TestPanicRecovery(t *testing.T) {
	var logBuffer bytes.Buffer
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
