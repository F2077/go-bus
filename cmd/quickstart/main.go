// Command quickstart is a minimal, end-to-end tour of go-bus's public API.
//
// It exercises every exported surface in one runnable file:
//
//	NewBus + WithLogger   — construct a bus, inject an slog.Logger
//	NewEvent              — typed event keys from iota constants
//	NewListener + On      — register listeners on an event (fan-out)
//	WithOnetime           — a listener that fires exactly once
//	Emit                  — non-blocking dispatch
//	Cancel / Close        — stop one listener, or all of them
//
// Listeners run asynchronously; the demo coordinates them with a channel (the
// correct pattern for production code), not time.Sleep.
//
// Run with: go run ./cmd/quickstart
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/F2077/go-bus/bus"
)

func main() {
	// 1. Create a bus. WithLogger injects an slog.Logger; recovered listener
	//    panics land here. Omit the option for a quiet default logger.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	eventBus, err := bus.NewBus(bus.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create bus", "error", err)
		return
	}
	defer eventBus.Close() // cancel every listener on exit

	// 2. Define events as typed constants, then wrap each with NewEvent.
	const (
		ConfigReloaded = iota + 1
		ThresholdBreached
	)
	reload := bus.NewEvent(ConfigReloaded)
	alert := bus.NewEvent(ThresholdBreached)

	// 3. Coordinate async listeners with a channel (not a sleep): each
	//    listener signals done once it has handled a message.
	done := make(chan struct{}, 4) // 至多 4 个并发监听器
	signal := func() { done <- struct{}{} }
	drain := func(n int) { for i := 0; i < n; i++ { <-done } }

	// 4. Multiple listeners may subscribe to the same event — Emit fans out
	//    to all of them. Each runs in its own goroutine.
	audit := bus.NewListener(func(msg any) {
		fmt.Printf("  [audit]     config reloaded: %#v\n", msg)
		signal()
	})
	cache := bus.NewListener(func(msg any) {
		fmt.Println("  [cache]     invalidated all entries")
		signal()
	})
	if err := eventBus.On(reload, audit); err != nil {
		logger.Error("subscribe audit", "error", err)
		return
	}
	if err := eventBus.On(reload, cache); err != nil {
		logger.Error("subscribe cache", "error", err)
		return
	}

	// 5. A one-shot listener: WithOnetime auto-removes it after its first fire.
	bootstrap := bus.NewListener(func(msg any) {
		fmt.Println("  [bootstrap] first reload — running migrations (once)")
		signal()
	}, bus.WithOnetime(true))
	if err := eventBus.On(reload, bootstrap); err != nil {
		logger.Error("subscribe bootstrap", "error", err)
		return
	}

	// A different event carries its own, independent listeners.
	alerter := bus.NewListener(func(msg any) {
		fmt.Printf("  [alerter]   threshold breached: %#v\n", msg)
		signal()
	})
	if err := eventBus.On(alert, alerter); err != nil {
		logger.Error("subscribe alerter", "error", err)
		return
	}

	// 6. Emit #1 — fans out to audit, cache, and the one-shot bootstrap.
	fmt.Println("emit #1  ConfigReloaded(\"new.yaml\") — all three fire:")
	if err := eventBus.Emit(reload, "new.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	drain(3)

	// 7. Emit #2 — audit and cache fire again; bootstrap stays silent.
	fmt.Println("emit #2  ConfigReloaded(\"prod.yaml\") — bootstrap is gone:")
	if err := eventBus.Emit(reload, "prod.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	drain(2)

	// 8. Cancel one listener — subsequent emits skip it.
	cache.Cancel()
	fmt.Println("emit #3  ConfigReloaded(\"canary.yaml\") — cache cancelled, only audit:")
	if err := eventBus.Emit(reload, "canary.yaml"); err != nil {
		logger.Error("emit", "error", err)
	}
	drain(1)

	// 9. A different event reaches only its own listeners.
	fmt.Println("emit #4  ThresholdBreached(\"cpu 94%\") — alerter only:")
	if err := eventBus.Emit(alert, "cpu 94%"); err != nil {
		logger.Error("emit", "error", err)
	}
	drain(1)

	fmt.Println("demo complete")
}
