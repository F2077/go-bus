package bus_test

import (
	"fmt"

	"github.com/F2077/go-bus/bus"
)

// ExampleNewBus shows the minimal setup: create an event bus with the default
// logger, ready to register listeners and emit events.
func ExampleNewBus() {
	eventBus, err := bus.NewBus()
	if err != nil {
		panic(err)
	}
	defer eventBus.Close()
	fmt.Println(eventBus != nil)
	// Output: true
}

// ExampleBus_On demonstrates the publish/subscribe round-trip: register a
// listener on a typed Event, emit a payload, and receive it synchronously by
// funnelling the callback through a channel.
func ExampleBus_On() {
	eventBus, _ := bus.NewBus()
	defer eventBus.Close()

	received := make(chan any, 1)
	listener := bus.NewListener(func(msg any) {
		received <- msg
	})
	if err := eventBus.On(bus.NewEvent(1), listener); err != nil {
		panic(err)
	}
	if err := eventBus.Emit(bus.NewEvent(1), "hello"); err != nil {
		panic(err)
	}

	fmt.Println(<-received)
	// Output: hello
}

// ExampleWithOnetime shows a one-time listener: it fires on the first matching
// event and then auto-removes, so subsequent emits on the same event are
// ignored.
func ExampleWithOnetime() {
	eventBus, _ := bus.NewBus()
	defer eventBus.Close()

	received := make(chan any, 2)
	listener := bus.NewListener(func(msg any) {
		received <- msg
	}, bus.WithOnetime(true))
	if err := eventBus.On(bus.NewEvent(1), listener); err != nil {
		panic(err)
	}

	_ = eventBus.Emit(bus.NewEvent(1), "first")
	_ = eventBus.Emit(bus.NewEvent(1), "second") // ignored: listener already removed

	fmt.Println(<-received)
	// Output: first
}
