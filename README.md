# efsm

[![Go Reference](https://pkg.go.dev/badge/github.com/webermarci/efsm.svg)](https://pkg.go.dev/github.com/webermarci/efsm)
[![Test](https://github.com/webermarci/efsm/actions/workflows/test.yml/badge.svg)](https://github.com/webermarci/efsm/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`efsm` is a small, generic, thread-safe extended finite state machine for Go.
It provides immutable, constructor-time configuration for states, events,
guards, redirects, and effects.

## Features

- **Immutable configuration:** The transition graph is frozen by `NewStateMachine`.
- **Type-safe states and events:** State and event types are generic and comparable.
- **Safe concurrency:** Transitions are serialized; current-state and inspection reads are safe concurrently.
- **Dry-run validation:** Use `Check` to run guards and redirects without changing state or running effects.
- **Reusable options:** State behavior is grouped with `WithState`, while individual options can be reused across states.
- **Guards and effects:** Use `WithGuard`, `OnEntry`, `OnExit`, and `OnTransition`.
- **Dynamic routing:** Use `WithPermitRedirect` when the target depends on event data.

## Installation

```bash
go get github.com/webermarci/efsm
```

The module requires Go 1.26.2 or newer.

## Quick start

```go
package main

import (
	"fmt"

	"github.com/webermarci/efsm"
)

type State string
type Event string

const (
	Disconnected State = "disconnected"
	Connecting   State = "connecting"
	Connected    State = "connected"
	Failed       State = "failed"

	Connect    Event = "connect"
	Success    Event = "success"
	Failure    Event = "failure"
	Reset      Event = "reset"
)

type Data struct {
	RetryCount int
}

func main() {
	telemetryEntry := efsm.OnEntry(func(t efsm.Transition[State, Event], _ Data) {
		fmt.Println("entered", t.To)
	})
	
	telemetryExit := efsm.OnExit(func(t efsm.Transition[State, Event], _ Data) {
		fmt.Println("exited", t.From)
	})

	sm := efsm.NewStateMachine[State, Event, Data](
		Disconnected,

		efsm.WithState(Disconnected,
			telemetryEntry,
			telemetryExit,
			efsm.WithPermit(Connect, Connecting),
		),

		efsm.WithState(Connecting,
			telemetryEntry,
			telemetryExit,
			efsm.WithPermit(Success, Connected),
			efsm.WithPermitRedirect(Failure, func(_ efsm.Transition[State, Event], data Data) State {
				if data.RetryCount > 2 {
					return Failed
				}
				return Disconnected
			}),
		),

		efsm.WithState(Connected,
			telemetryEntry,
			telemetryExit,
			efsm.WithPermit(Reset, Disconnected),
		),

		efsm.WithState(Failed,
			efsm.WithPermit(Reset, Disconnected),
		),
	)

	if _, err := sm.Fire(Connect, Data{}); err != nil {
		panic(err)
	}

	if _, err := sm.Fire(Failure, Data{RetryCount: 3}); err != nil {
		// handle a rejected transition
	}

	fmt.Println("final state:", sm.CurrentState())
}
```

`WithState` keeps all behavior for a state together. State options are ordinary
Go values, so reusable behavior is explicit:

```go
var entryLog = efsm.OnEntry(logEntry)
var exitLog = efsm.OnExit(logExit)
var disconnect = efsm.WithPermit(EventDisconnect, StateDisconnected)

efsm.WithState(StateConnecting, entryLog, exitLog, disconnect)
efsm.WithState(StateConnected, entryLog, exitLog, disconnect)
```

If the same event is permitted more than once for a state, the last permit
replaces the earlier rule. Fixed permit targets are registered automatically,
but a target returned by `WithPermitRedirect` must have its own `WithState`
declaration. Redirecting to the current state is valid and produces a
self-transition.

Use `Check` when you need to validate an event before firing it. It runs the
same guard and redirect logic as `Fire`, returns the resolved `Transition`, and
leaves the current state and all effects unchanged:

```go
transition, err := sm.Check(Connect, Data{})
```

`Check` and a later `Fire` are separate operations, so the state may change
between them. If validation and the state change must be atomic, call `Fire`
and handle its returned error.

Callback options must use the machine's data type. For example, a machine
constructed with `Data` should use `OnEntry`, `OnExit`, `WithGuard`, and
`WithPermitRedirect` callbacks that also accept `Data`. A non-nil value with a
mismatched callback type panics when that callback runs.

For a state with no behavior of its own, specify its state and event types so
Go can infer the otherwise-unused event type:

```go
efsm.WithState[State, Event](StateFailed)
```

Fixed permit targets are registered automatically. Redirect targets must be
declared with `WithState`; redirecting to an unknown state returns
`efsm.ErrUnknownState` and leaves the machine unchanged.

## Concurrency

`Check` serializes event validation, guards, and redirect resolution without
changing state or running effects. `Fire` serializes the transition, guard, and
effects. The state is committed before effects run, and effects execute in this
order:

1. source `OnExit` effects
2. permit `OnTransition` effect
3. target `OnEntry` effects

Callbacks are synchronous and must not call `Check` or `Fire` recursively. Keep
queues, actor loops, shutdown, and long-running work in the surrounding
application or runtime, such as `sup`.

`CurrentState`, `AvailableStates`, `AvailableEvents`, and
`AvailableEventsForStates` are safe to call concurrently with `Check` or
`Fire`.
`AvailableEventsForStates` includes only states that have at least one event;
the returned map and slices can be modified by the caller without changing the
machine.

## Errors

`Check` and `Fire` return the attempted `Transition` even when they return an
error. Use the returned error to distinguish an unavailable event, an unknown
redirect target, or a guard rejection.

Use `errors.Is` with these sentinel errors:

- `ErrInvalidEvent`
- `ErrUnknownState`

Invalid programmer configuration, such as nil options or callbacks, panics
during construction. There is no mutable configuration API after construction.
