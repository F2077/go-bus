[![Go Reference](https://pkg.go.dev/badge/github.com/F2077/go-bus.svg)](https://pkg.go.dev/github.com/F2077/go-bus)
[![Go Report Card](https://goreportcard.com/badge/github.com/F2077/go-bus)](https://goreportcard.com/report/github.com/F2077/go-bus)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Coverage](https://img.shields.io/badge/coverage-95.7%25-brightgreen)](#performance)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

# go-bus

A minimal, **in-process** event bus for Go — typed events, cancelable listeners,
and one-shot handlers, built on top of the zero-allocation
[go-pubsub](https://github.com/F2077/go-pubsub) engine.

> 🔔 go-bus is **in-process** and **fire-and-forget**. It is the right tool for
> decoupling components *inside a single Go binary*. If you need to cross a
> process or network boundary, reach for NATS, Kafka, or RabbitMQ instead.

---

## Why go-bus?

| | |
|---|---|
| 🎯 **Typed events** | Dispatch arbitrary `any` payloads keyed by a typed `Event`. |
| ⚡ **Zero-alloc emits** | The publish hot path allocates nothing — see [Performance](#performance). |
| 🔄 **One-shot handlers** | Listeners can auto-remove after their first trigger. |
| 🎛️ **Context-based cancel** | Every listener owns a `context.Context`; cancel stops it cleanly. |
| 🛡️ **Panic-safe** | A panicking listener is recovered and logged; the bus keeps running. |
| 🔌 **Thin layer** | A focused wrapper over [go-pubsub](https://github.com/F2077/go-pubsub) — no hidden runtime, no goroutine zoo to manage. |

---

## When to use it

### ✅ A great fit

- **Domain / application events** — *"user signed up"*, *"order created"* fanned
  out to multiple handlers within one process.
- **Decoupling producers from consumers** — emit without knowing who listens;
  react without importing the emitter.
- **One-shot hooks** — *"run this once on first connection"*, then auto-remove.
- **Lightweight fan-out** — notify *N* subscribers per emit, allocation-free on
  the hot path.

### ❌ Not a good fit

- **Cross-process / distributed pub-sub** — go-bus has no network protocol.
  Use NATS, Kafka, or RabbitMQ.
- **Durable queues or at-least-once delivery** — a message lives only inside a
  subscriber's buffered channel. There is **no persistence and no redelivery**.
- **Strict reliability** — if a subscriber falls behind and its channel fills,
  **messages are dropped silently**. This is fire-and-forget *by design*.
- **Backpressure / acknowledgement** — `Emit` never blocks and there is no ack
  path. Successful return means only "the broker accepted the message".

---

## Installation

```bash
go get github.com/F2077/go-bus
```

Requires **Go 1.25+** (the underlying `go-pubsub` uses the `tool` directive).

---

## Quick start

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/F2077/go-bus/bus"
)

func main() {
	// 1. Create a bus (optionally inject your own logger).
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	eventBus, err := bus.NewBus(bus.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create bus", "error", err)
		return
	}

	// 2. Define events as typed constants.
	const (
		ConfigReloaded = iota + 1
		ThresholdBreached
	)
	reload := bus.NewEvent(ConfigReloaded)

	// 3. Register a listener.
	listener := bus.NewListener(func(msg any) {
		fmt.Printf("config reloaded: %#v\n", msg)
	})
	if err := eventBus.On(reload, listener); err != nil {
		logger.Error("failed to subscribe", "error", err)
		return
	}

	// 4. Emit an event.
	if err := eventBus.Emit(reload, "new.yaml"); err != nil {
		logger.Error("failed to emit", "error", err)
	}

	// 5. Give the listener a moment, then stop it.
	time.Sleep(100 * time.Millisecond)
	listener.Cancel()

	fmt.Println("demo complete")
}
```

A runnable version of the above lives at [`cmd/quickstart`](./cmd/quickstart).

---

## Usage

### Events

An `Event` is a named integer. Use `iota` constants to keep them tidy:

```go
const (
	ConfigReloaded = iota + 1
	ThresholdBreached
	ConnectionDropped
)

event := bus.NewEvent(ConfigReloaded)
```

### Listening

`NewListener` takes a callback (`func(msg any)`) and optional options.
`On` registers it; the returned listener can be `Cancel`ed at any time.

```go
listener := bus.NewListener(func(msg any) {
	// handle msg
})
_ = eventBus.On(bus.NewEvent(ThresholdBreached), listener)

// ...later, stop receiving:
listener.Cancel()
```

### One-shot listeners

Pass `WithOnetime(true)` for a handler that fires **exactly once** and then
auto-removes — handy for "do this on the first occurrence":

```go
once := bus.NewListener(func(msg any) {
	fmt.Println("first connection dropped — paging on-call")
}, bus.WithOnetime(true))
_ = eventBus.On(bus.NewEvent(ConnectionDropped), once)
```

### Emitting

`Emit` is non-blocking and never panics on a slow consumer — it dispatches to
every current listener on that event. The publish path is allocation-free.

```go
_ = eventBus.Emit(bus.NewEvent(ThresholdBreached), "cpu 94%")
```

### Custom logger

By default go-bus writes to a quiet `slog` text handler at `Error` level (this
is where recovered listener panics land). Inject your own:

```go
eventBus, _ := bus.NewBus(bus.WithLogger(myLogger))
```

---

## How it works

go-bus is a thin layer over [go-pubsub](https://github.com/F2077/go-pubsub). The
delivery model is deliberately simple:

- **Fan-out, not queueing.** One `Emit` delivers to every listener currently
  subscribed to that event.
- **Buffered, drop-on-full.** Each listener drains a buffered channel. If the
  channel is full when a message arrives, **that message is dropped for that
  listener** — there is no backpressure and no redelivery.
- **Per-listener goroutine.** `On` starts one goroutine that runs the callback
  until the listener is canceled, becomes one-shot and fires, or the bus's
  subscription channel closes.
- **Panic isolation.** A panicking callback is recovered and logged through the
  bus logger; siblings and the bus itself are unaffected.

For finer-grained control (per-topic channel sizing, sliding idle timeouts,
introspection), use [go-pubsub](https://github.com/F2077/go-pubsub) directly.

---

## Performance

Measured with `go test -bench=. -benchmem -benchtime=1s` on an 18-core Linux
machine, Go 1.25. Numbers are indicative — treat them as relative, not absolute.

| Benchmark | Setup | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `BenchmarkEventBus` | 1000 listeners, parallel emit | **228** | 0 | **0** |
| `BenchmarkFanout/subs=1` | 1 listener | 236 | 0 | 0 |
| `BenchmarkFanout/subs=10` | 10 listeners | 1,460 | 0 | 0 |
| `BenchmarkFanout/subs=100` | 100 listeners | 5,018 | 0 | 0 |
| `BenchmarkFanout/subs=1000` | 1000 listeners | 36,732 | 10 | 0 |
| `BenchmarkOneTimeListener` | On + Emit + auto-remove | 5,791 | 4,128 | 20 |

**Takeaways**

- The emit hot path is **zero-allocation** up to 1000 subscribers — the reusable
  publisher plus `go-pubsub`'s `sync.Pool` fan-out snapshot keep it clean.
- Fan-out cost scales ~linearly with subscriber count, as expected for a
  per-subscriber delivery loop.
- `OneTimeListener` is the only allocating path, because each registration builds
  a `Listener`, a `context.Context`, and a fresh subscriber handle.

Reproduce locally:

```bash
go test -run='^$' -bench=. -benchmem -benchtime=1s ./bus/
```

---

## License

[MIT](LICENSE). Built on [go-pubsub](https://github.com/F2077/go-pubsub) (also MIT).
