# efsm

[![Go Reference](https://pkg.go.dev/badge/github.com/webermarci/efsm.svg)](https://pkg.go.dev/github.com/webermarci/efsm)
[![Test](https://github.com/webermarci/efsm/actions/workflows/test.yml/badge.svg)](https://github.com/webermarci/efsm/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`efsm` is a generic, thread-safe, extended finite state machine (FSM) for Go. It provides a fluent builder API to define states, events, and transition guards, making it easy to model complex logic safely in concurrent environments.

It is relentlessly optimized for high-throughput, highly concurrent environments. It features a zero-allocation pointer-graph architecture, 100% lock-free reads, and CPU cache-line padding to prevent false sharing.

## Features

- Type-safe states and events using Go generics.
- Thread-safe state transitions with 100% lock-free reads and mutually exclusive writes.
- Fluent builder pattern for clean and intuitive configuration.
- Transition guards that support custom data payloads.
- Dynamic conditional routing at runtime (`WithRedirect`).

## Quick start

```go
package main

import (
	"errors"
	"fmt"

	"github.com/webermarci/efsm"
)

// 1. Define states and events as strongly typed aliases
type State string
type Event string

const (
	StateDisconnected State = "Disconnected"
	StateConnecting   State = "Connecting"
	StateConnected    State = "Connected"
	StateFailed       State = "Failed"

	EventConnect    Event = "Connect"
	EventDisconnect Event = "Disconnect"
	EventSuccess    Event = "Success"
	EventError      Event = "Error"
)

// 2. Define the generic data payload passed to hooks
type Data struct {
	RetryCount int
	IPAddress  string
}

func main() {
	// Initialize the state machine with the starting state
	sm := efsm.NewStateMachine[State, Event, Data](StateDisconnected)

	// 3. Declaratively configure the state rules and transitions
	sm.State(StateDisconnected).
		OnEntry(func(t efsm.Transition[State, Event], d Data) {
			fmt.Println("🔌 Hook: Device is safely disconnected.")
		}).
		Permit(EventConnect, StateConnecting)

	sm.State(StateConnecting).
		OnEntry(func(t efsm.Transition[State, Event], d Data) {
			fmt.Printf("⏳ Hook: Attempting connection to %s...\n", d.IPAddress)
		}).
		// Use a Guard to reject the transition if parameters are invalid
		Permit(EventSuccess, StateConnected, efsm.WithGuard(
			func(t efsm.Transition[State, Event], d Data) error {
				if d.IPAddress == "" {
					return errors.New("missing IP address")
				}
				return nil
			},
		)).
		// Use a dynamic redirect to evaluate runtime data and choose the target state
		Permit(EventError, StateDisconnected, efsm.WithRedirect(
			func(t efsm.Transition[State, Event], d Data) State {
				if d.RetryCount >= 3 {
					return StateFailed // Out of retries, route to Failed state
				}
				return t.To // Fallback to default target (Disconnected)
			},
		)).
		// Add an effect specific to this transition
		Permit(EventDisconnect, StateDisconnected, efsm.OnTransition(
			func(t efsm.Transition[State, Event], d Data) {
				fmt.Println("⚠️ Effect: Connection aborted by user.")
			},
		))

	sm.State(StateConnected).
		OnEntry(func(t efsm.Transition[State, Event], d Data) {
			fmt.Println("✅ Hook: Connection established successfully!")
		}).
		Permit(EventDisconnect, StateDisconnected)

	sm.State(StateFailed).
		OnEntry(func(t efsm.Transition[State, Event], d Data) {
			fmt.Println("🚨 Hook: Connection failed permanently.")
		}).
		Permit(EventConnect, StateConnecting) // Allow manual retry

	// 4. Execute the state machine
	payload := Data{RetryCount: 3, IPAddress: "192.168.1.100"}

	fmt.Printf("Initial State: %s\n", sm.CurrentState())

	// Fire: Disconnected -> Connecting
	_ = sm.Fire(EventConnect, payload)
	fmt.Printf("Current State: %s\n\n", sm.CurrentState())

	// Fire: Connecting -> Failed (Redirect dynamically overrides Disconnected)
	_ = sm.Fire(EventError, payload)
	fmt.Printf("Current State: %s\n\n", sm.CurrentState())
}
```

## Benchmark

```bash
goos: darwin
goarch: arm64
pkg: github.com/webermarci/efsm
cpu: Apple M5
BenchmarkStateMachine_Fire-10                      152293412   7.63 ns/op  0 B/op  0 allocs/op
BenchmarkStateMachine_FireWithEffects-10           144341641   8.32 ns/op  0 B/op  0 allocs/op
BenchmarkStateMachine_State_Parallel-10           1000000000   0.20 ns/op  0 B/op  0 allocs/op
BenchmarkStateMachine_Fire_Parallel-10              19826025  59.61 ns/op  0 B/op  0 allocs/op
BenchmarkStateMachine_FireWithRedirect-10          122129552  9.819 ns/op  0 B/op  0 allocs/op
BenchmarkStateMachine_FireWithRedirect_Parallel-10  17619495  68.29 ns/op  0 B/op  0 allocs/op
```
