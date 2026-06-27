// Command quickstart is a minimal, end-to-end tour of go-bus's public API.
//
// It exercises every exported surface in one runnable file:
//
//	NewBus + WithLogger   — construct a bus, inject an slog handler
//	NewEvent              — typed event keys from iota constants
//	NewListener + On      — register listeners on an event (fan-out)
//	WithOnetime           — a listener that fires exactly once
//	Emit                  — non-blocking, zero-allocation dispatch
//	Cancel                — stop a listener via its context
//
// Listeners run asynchronously, so the program paces them with a short sleep.
// In production code, coordinate with channels or a sync.WaitGroup instead.
//
// Run with: go run ./cmd/quickstart
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/F2077/go-bus/bus"
)

func main() {
	// 1. Create a bus. WithLogger injects an slog handler; recovered listener
	//    panics land here. Omit the option for a quiet default logger.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	eventBus, err := bus.NewBus(bus.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create bus", "error", err)
		return
	}

	// 2. Define events as typed constants, then wrap each with NewEvent.
	const (
		ConfigReloaded = iota + 1
		ThresholdBreached
	)
	reload := bus.NewEvent(ConfigReloaded)
	alert := bus.NewEvent(ThresholdBreached)

	// 3. Multiple listeners may subscribe to the same event — Emit fans out
	//    to all of them. Each runs in its own goroutine.
	audit := bus.NewListener(func(msg any) {
		fmt.Printf("  [audit]     config reloaded: %#v\n", msg)
	})
	cache := bus.NewListener(func(msg any) {
		fmt.Println("  [cache]     invalidated all entries")
	})
	if err := eventBus.On(reload, audit); err != nil {
		logger.Error("subscribe audit", "error", err)
		return
	}
	if err := eventBus.On(reload, cache); err != nil {
		logger.Error("subscribe cache", "error", err)
		return
	}

	// 4. A one-shot listener: WithOnetime auto-removes it after its first fire.
	bootstrap := bus.NewListener(func(msg any) {
		fmt.Println("  [bootstrap] first reload — running migrations (once)")
	}, bus.WithOnetime(true))
	if err := eventBus.On(reload, bootstrap); err != nil {
		logger.Error("subscribe bootstrap", "error", err)
		return
	}

	// A different event carries its own, independent listeners.
	if err := eventBus.On(alert, bus.NewListener(func(msg any) {
		fmt.Printf("  [alerter]   threshold breached: %#v\n", msg)
	})); err != nil {
		logger.Error("subscribe alerter", "error", err)
		return
	}

	// 5. Emit #1 — dispatches to every reload listener, including the
	//    one-shot bootstrap (its only chance to fire).
	fmt.Println("emit #1  ConfigReloaded(\"new.yaml\") — all three listeners fire:")
	if err := eventBus.Emit(reload, "new.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	pace()

	// 6. Emit #2 — audit and cache fire again; bootstrap stays silent,
	//    proving the one-shot listener already removed itself.
	fmt.Println("emit #2  ConfigReloaded(\"prod.yaml\") — bootstrap is gone, the other two fire:")
	if err := eventBus.Emit(reload, "prod.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	pace()

	// 7. Cancel a listener — its goroutine stops; subsequent emits skip it.
	cache.Cancel()
	fmt.Println("emit #3  ConfigReloaded(\"canary.yaml\") — cache cancelled, only audit remains:")
	if err := eventBus.Emit(reload, "canary.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	pace()

	// 8. A different event reaches only its own listeners.
	fmt.Println("emit #4  ThresholdBreached(\"cpu 94%\") — a separate event, alerter only:")
	if err := eventBus.Emit(alert, "cpu 94%"); err != nil {
		logger.Error("emit", "error", err)
	}
	pace()

	fmt.Println("demo complete")
}

// pace gives asynchronous listener goroutines a beat to print before main
// moves on. Real code should use a channel or sync.WaitGroup instead.
func pace() { time.Sleep(50 * time.Millisecond) }
