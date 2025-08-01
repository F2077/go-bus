# go-bus

A simple event bus library for Go, built on top of [go-pubsub](https://github.com/F2077/go-pubsub).
Ideal for application-level event dispatching (e.g. user actions, domain events), with convenient listener management and one-time handlers.

---

## Features

* 🔔 **Event-based**: Dispatch arbitrary `any` payloads keyed by typed `Event`.
* 🔄 **Auto-unsubscribe**: One-time listeners auto-remove after first trigger.
* 🎛️ **Listener control**: Cancelable via context.

---

## Installation

```bash
go get github.com/F2077/go-bus
```

## Quick Start

```go
package main

import (
	"fmt"
	"github.com/F2077/go-bus/bus"
	"log/slog"
	"os"
	"time"
)

func main() {
	// 1. Create an Event Bus (optionally inject a custom logger)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	eventBus, err := bus.NewBus(bus.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create bus", "error", err)
		return
	}

	// 2. Define some event constants
	const (
		UserSignedUp = iota + 1
		OrderCreated
	)
	// Convert integer to Event type
	signupEvent := bus.NewEvent(UserSignedUp)

	// 3. Register a listener callback
	listener := bus.NewListener(func(msg any) {
		fmt.Printf("Received signup: %#v\n", msg)
	})
	if err := eventBus.On(signupEvent, listener); err != nil {
		logger.Error("failed to subscribe", "error", err)
		return
	}

	// 4. Emit an event
	payload := map[string]string{"user": "alice", "plan": "pro"}
	if err := eventBus.Emit(signupEvent, payload); err != nil {
		logger.Error("failed to emit event", "error", err)
	}

	// 5. Give the listener a moment to process
	time.Sleep(100 * time.Millisecond)

	// 6. Cancel the listener
	listener.Cancel()

	// 7. Exit gracefully
	fmt.Println("demo complete")
}
```

## Notes

This library is built on top of go-pubsub:

    It uses go-pubsub as the underlying high-performance Pub/Sub engine.

    Adds application-level conveniences: typed Event, automatic unsubscription, listener context control, etc.

If you need finer-grained Pub-Sub control, check out the original [go-pubsub](https://github.com/F2077/go-pubsub) library.
