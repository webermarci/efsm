# efsm

[![Go Reference](https://pkg.go.dev/badge/github.com/webermarci/efsm.svg)](https://pkg.go.dev/github.com/webermarci/efsm)
[![Test](https://github.com/webermarci/efsm/actions/workflows/test.yml/badge.svg)](https://github.com/webermarci/efsm/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`efsm` is a generic, thread-safe, extended finite state machine (EFSM) for Go. It provides a fluent builder API to define states, events, and transition guards, making it easy to model complex logic safely in concurrent environments.

It is relentlessly optimized for high-throughput, highly concurrent environments. It features a zero-allocation pointer-graph architecture, 100% lock-free reads, and CPU cache-line padding to prevent false sharing.

## Features

- **Zero-Allocation Hot Path:** Transitioning states (`Fire`) requires 0 heap allocations.
- **Type-Safe Generics:** Define your own state and event types without empty interfaces (`interface{}` or `any`).
- **Highly Concurrent:** 100% lock-free reads (`CurrentState`), mutually exclusive writes, and CPU cache-line padding to prevent false sharing.
- **Composable Mixins:** Use the `Configure` API to write reusable state logic (like telemetry or error handling) and apply it across multiple states.
- **Dynamic Routing:** Resolve target states dynamically at runtime based on data context (`PermitRedirect`).
- **Guards & Effects:** Hook into state transitions with `WithGuard`, `OnEntry`, `OnExit`, and `OnTransition`.

## Installation

Requires Go 1.18 or later (uses Generics).

```bash
go get github.com/webermarci/efsm
```

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
	EventTimeout    Event = "Timeout"
)

type Data struct {
	RetryCount int
	IPAddress  string
}

func main() {
	sm := efsm.NewStateMachine[State, Event, Data](StateDisconnected)

	// --- 2. Define Reusable Mixins ---
	// Mixins can define standard logging, telemetry, or shared transitions.
	// Their hooks will execute alongside the state-specific hooks!
	withTelemetry := func(c *efsm.StateConfigurator[State, Event, Data]) {
		c.OnEntry(func(t efsm.Transition[State, Event], d Data) {
			fmt.Printf("[Telemetry] Entered state: %s\n", t.To)
		})
		
		c.Permit(EventDisconnect, StateDisconnected, efsm.OnTransition(
			func(t efsm.Transition[State, Event], d Data) {
				fmt.Println("[Telemetry] Connection cleanly aborted.")
			},
		))
	}

	// --- 3. Configure States ---
	sm.Configure(StateDisconnected)

	sm.Configure(StateConnecting, 
		withTelemetry, 
		func(c *efsm.StateConfigurator[State, Event, Data]) {
			// This OnEntry runs right after the telemetry OnEntry
			c.OnEntry(func(t efsm.Transition[State, Event], d Data) {
				fmt.Printf("⏳ Attempting connection to %s...\n", d.IPAddress)
			})

			c.Permit(EventSuccess, StateConnected, efsm.WithGuard(
				func(t efsm.Transition[State, Event], d Data) error {
					if d.IPAddress == "" {
						return errors.New("missing IP address")
					}
					return nil
				},
			))

			// Dynamic redirect based on runtime context
			c.PermitRedirect(EventError, func(t efsm.Transition[State, Event], d Data) State {
				if d.RetryCount >= 3 {
					return StateFailed
				}
				return StateDisconnected
			})

			for _, event := range []Event{EventTimeout} {
				c.Permit(event, StateFailed)
			}
		},
	)

	sm.Configure(StateConnected, 
		withTelemetry,
		func(c *efsm.StateConfigurator[State, Event, Data]) {
			c.OnEntry(func(t efsm.Transition[State, Event], d Data) {
				fmt.Println("✅ Connection established successfully!")
			})
		},
	)

	sm.Configure(StateFailed, func(c *efsm.StateConfigurator[State, Event, Data]) {
		c.Permit(EventConnect, StateConnecting)
	})

	// --- 4. Execute ---
	payload := Data{RetryCount: 3, IPAddress: "192.168.1.100"}

	// Use CanFire to check if an action is valid before trying it (great for UI rendering)
	if sm.CanFire(EventConnect) {
		fmt.Println("Button 'Connect' is enabled.")
	}

	// Use MustFire when you are programmatically certain the event is valid
	// and want a panic on developer error instead of checking err != nil
	sm.MustFire(EventConnect, payload)

	// Normal Fire for events that might be rejected by a guard or state mismatch
	err := sm.Fire(EventError, payload)
	if err != nil {
		fmt.Printf("Failed to fire: %v\n", err)
	}
	
	fmt.Printf("\nFinal State: %s\n", sm.CurrentState())
}
```

## Important Notes on Concurrency

To guarantee that state transitions are strictly atomic and ordered, `efsm` holds an internal lock during the execution of a transition and its associated effects (`OnExit`, `OnTransition`, `OnEntry`).

**⚠️ Warning:** You **MUST NOT** call `sm.Fire()`, `sm.CanFire()`, or `sm.MustFire()` synchronously from within an effect or guard. Doing so will cause a deadlock.

If an entry effect needs to trigger a subsequent state transition, you must spawn a new goroutine to push the event to the back of the line:
```go
sm.Configure(StateConnecting, func(c *efsm.StateConfigurator[State, Event, Data]) {
    c.OnEntry(func(t efsm.Transition[State, Event], d Data) {
        // Correct: Run async so the current transition can finish and release the lock
        go func() {
            err := connectToNetwork(d.IPAddress)
            if err != nil {
                sm.Fire(EventError, d)
            } else {
                sm.Fire(EventSuccess, d)
            }
        }()
    })
})
```

## Benchmark

```bash
goos: darwin
goarch: arm64
pkg: github.com/webermarci/efsm
cpu: Apple M5
BenchmarkStateMachine_Fire-10                   124017261    9.43 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_FireEffects-10            121889860    9.86 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_State_Parallel-10        1000000000    0.19 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_Fire_Parallel-10           17264893   68.48 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_FireRedirect-10           100000000   11.50 ns/op   0 B/op   0 allocs/op
BenchmarkStateMachine_FireRedirect_Parallel-10   17193207   69.46 ns/op   0 B/op   0 allocs/op
```
