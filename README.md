# efsm

[![Go Reference](https://pkg.go.dev/badge/github.com/webermarci/efsm.svg)](https://pkg.go.dev/github.com/webermarci/efsm)
[![Test](https://github.com/webermarci/efsm/actions/workflows/test.yml/badge.svg)](https://github.com/webermarci/efsm/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`efsm` is a generic, thread-safe, extended finite state machine (FSM) for Go. It provides a fluent builder API to define states, events, and transition guards, making it easy to model complex logic safely in concurrent environments.

### Features

- Type-safe states and events using Go generics.
- Thread-safe state transitions powered by read-write mutexes.
- Fluent builder pattern for clean and intuitive configuration.
- Context-aware transition guards that support custom data payloads.

### Quick start

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/webermarci/efsm"
)

// 1. Define states and events as strongly typed aliases
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

// 2. Define the generic data payload passed to hooks
type ConnectionData struct {
	RetryCount int
	IPAddress  string
}

func main() {
	// Initialize the state machine with the starting state
	sm := efsm.NewStateMachine[State, Event, ConnectionData](StateDisconnected)

	// 3. Declaratively configure the state rules and transitions
	sm.State(StateDisconnected).
		OnEntry(func(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) {
			fmt.Println("🔌 Hook: Device is safely disconnected.")
		}).
		Permit(EventConnect, StateConnecting)

	sm.State(StateConnecting).
		OnEntry(func(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) {
			fmt.Printf("⏳ Hook: Attempting connection to %s...\n", d.IPAddress)
		}).
		// Use a Guard to reject the transition if max retries are exceeded
		Permit(EventSuccess, StateConnected, efsm.WithGuard(
			func(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) error {
				if d.RetryCount > 3 {
					return errors.New("max connection retries exceeded")
				}
				return nil
			},
		)).
		// Add an effect specific to this transition
		Permit(EventDisconnect, StateDisconnected, efsm.WithEffect(
			func(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) {
				fmt.Println("⚠️ Effect: Connection aborted by user.")
			},
		))

	sm.State(StateConnected).
		OnEntry(func(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) {
			fmt.Println("✅ Hook: Connection established successfully!")
		}).
		Permit(EventDisconnect, StateDisconnected)

	// 4. Execute the state machine
	ctx := context.Background()
	payload := ConnectionData{RetryCount: 1, IPAddress: "192.168.1.100"}

	fmt.Printf("Initial State: %s\n", sm.CurrentState())

	// Fire: Disconnected -> Connecting
	_ = sm.Fire(ctx, EventConnect, payload)
	fmt.Printf("Current State: %s\n\n", sm.CurrentState())

	// Fire: Connecting -> Connected (Guard passes)
	_ = sm.Fire(ctx, EventSuccess, payload)
	fmt.Printf("Current State: %s\n\n", sm.CurrentState())

	// Test Guard failure scenario
	badPayload := ConnectionData{RetryCount: 5, IPAddress: "192.168.1.100"}
	err := sm.Fire(ctx, EventConnect, badPayload) // Invalid in Connected state
	if err != nil {
		fmt.Printf("Error: %v\n", err) // Output: event is not valid in current state: Connect in Connected
	}
}
```

### Benchmark

```bash
goos: darwin
goarch: arm64
pkg: github.com/webermarci/efsm
cpu: Apple M5
BenchmarkStateMachine_Fire-10             150355484   7.03 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_State_Parallel-10   14117120   85.50 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_Fire_Parallel-10    21855120   51.93 ns/op   3 B/op   0 allocs/op
```
