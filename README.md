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
	"fmt"
	"log"

	"github.com/webermarci/efsm"
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

func guard(ctx context.Context, t efsm.Transition[State, Event], d ConnectionData) error {
	fmt.Printf("Transitioning from %s to %s (Retries: %d)\n", t.From, t.To, d.RetryCount)
	return nil
}

func main() {
	sm := efsm.New[State, Event, ConnectionData](StateDisconnected).
		Permit(StateDisconnected, EventConnect, StateConnecting).
		Permit(StateConnecting, EventSuccess, StateConnected, efsm.WithGuard(guard)).
		Permit(StateConnected, EventDisconnect, StateDisconnected)

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
