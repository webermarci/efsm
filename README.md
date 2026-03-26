# efsm

[![Go Reference](https://pkg.go.dev/badge/github.com/webermarci/efsm.svg)](https://pkg.go.dev/github.com/webermarci/efsm)
[![Test](https://github.com/webermarci/efsm/actions/workflows/test.yml/badge.svg)](https://github.com/webermarci/efsm/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`efsm` is a generic, thread-safe, extended finite state machine (FSM) for Go. It provides a fluent builder API to define states, events, and transition actions, making it easy to model complex logic safely in concurrent environments.

### Features

- Type-safe states and events using Go generics.
- Thread-safe state transitions powered by read-write mutexes.
- Fluent builder pattern for clean and intuitive configuration.
- Context-aware transition actions that support custom data payloads.

### Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/yourusername/efsm"
)

// Define your states and events.
type State string
type Event string

const (
	StateDisconnected State = "Disconnected"
	StateConnecting   State = "Connecting"
	StateConnected    State = "Connected"

	EventConnect    Event = "Connect"
	EventDisconnect Event = "Disconnect"
	EventSuccess    Event = "Success"
)

// Define any custom data to pass along with transitions.
type ConnectionData struct {
	RetryCount int
}

func main() {
	builder := efsm.NewBuilder[State, Event, ConnectionData](StateDisconnected)

	builder.Configure(StateDisconnected).
		Permit(EventConnect, StateConnecting)

	builder.Configure(StateConnecting).
		PermitWithAction(
			EventSuccess,
			StateConnected,
			func(ctx context.Context, transition efsm.Transition[State, Event], data ConnectionData) error {
				fmt.Printf("Transitioning from %s to %s (Retries: %d)\n", transition.From, transition.To, data.RetryCount)
				return nil
			}
		).
		Permit(EventDisconnect, StateDisconnected)

	builder.Configure(StateConnected).
		Permit(EventDisconnect, StateDisconnected)

	sm := builder.Build()

	ctx := context.Background()
	data := ConnectionData{RetryCount: 1}

	if err := sm.Fire(ctx, EventConnect, data); err != nil {
		log.Fatalf("Transition failed: %v", err)
	}
	
	fmt.Printf("Current State: %s\n", sm.State()) // Output: Connecting

	if err := sm.Fire(ctx, EventSuccess, data); err != nil {
		log.Fatalf("Transition failed: %v", err)
	}
	
	fmt.Printf("Current State: %s\n", sm.State()) // Output: Connected
}
```
