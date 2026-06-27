// Package bus is a minimal, in-process event bus for Go, built on top of
// [github.com/F2077/go-pubsub].
//
// It dispatches arbitrary values keyed by a typed [Event] to cancelable
// [Listener] callbacks, with support for one-shot handlers. The emit path is
// allocation-free for an event the broker already holds; a brand-new event
// value allocates one topic slot.
//
// # Design constraints
//
// Package bus is intentionally fire-and-forget:
//
//   - In-process only. There is no network protocol; do not use it for
//     cross-process pub/sub.
//   - No delivery guarantees. Each listener drains a buffered channel; if the
//     channel is full when a message arrives, that message is dropped for that
//     listener — there is no acknowledgement path and no redelivery.
//   - Per-listener goroutine. [Bus.On] starts one goroutine that runs the
//     callback until the listener is canceled, becomes one-shot and fires, or
//     [Bus.Close] is called.
//   - Cancel races in-flight messages. If [Listener.Cancel] (or [Bus.Close])
//     lands between a message leaving the channel and its callback running,
//     that message is discarded so the cancel takes effect at once. A one-shot
//     listener can therefore fire zero times if its first message races with
//     cancellation.
//   - Panic isolation. A panicking callback is recovered and logged; the
//     listener keeps running on subsequent messages, and siblings plus the bus
//     itself are unaffected.
//
// # Events as topic keys
//
// Each [Event] value becomes a broker topic (its decimal string). The broker
// holds at most [pubsub.DefaultCapacity] (8192) topics, so keep Event as a
// small set of semantic constants and carry high-cardinality data (user IDs,
// timestamps) in the payload — not as event values. Raise the cap with
// [WithCapacity] when a bus genuinely fans out across more distinct events.
//
// # Typical usage
//
//	eventBus, _ := bus.NewBus()
//	defer eventBus.Close()
//	listener := bus.NewListener(func(msg any) { /* handle msg */ })
//	_ = eventBus.On(bus.NewEvent(1), listener)
//	_ = eventBus.Emit(bus.NewEvent(1), "payload")
//	listener.Cancel()
//
// For finer-grained control (per-topic channel sizing, sliding idle timeouts,
// introspection), use [github.com/F2077/go-pubsub] directly.
package bus
