// Package efsm provides a small, generic, thread-safe extended finite state
// machine for modeling workflows and stateful business logic.
//
// A StateMachine is configured once with functional options and then runs with
// Check and Fire. Its state graph is immutable after construction. Rules can
// include guards, dynamic redirects, transition effects, entry effects, and
// exit effects.
//
// # Getting started
//
// Define state and event types, group each state's behavior with WithState, and
// construct the machine:
//
//	type State string
//	type Event string
//
//	const (
//		Idle    State = "idle"
//		Running State = "running"
//		Start   Event = "start"
//		Stop    Event = "stop"
//	)
//
//	sm := efsm.NewStateMachine[State, Event, any](
//		Idle,
//		efsm.WithState(Idle, efsm.WithPermit(Start, Running)),
//		efsm.WithState(Running, efsm.WithPermit(Stop, Idle)),
//	)
//
//	transition, err := sm.Fire(Start, nil)
//	if err != nil {
//		// handle an invalid event or a rejected transition
//	}
//
//	state := transition.To
//
// Use Check to validate an event, including its guard and dynamic redirect,
// without changing the current state or running effects:
//
//	transition, err := sm.Check(Start, nil)
//
// State options are ordinary Go values and can be reused across WithState
// calls. For example, an entry effect can be stored once and passed to several
// states. Repeated permits for the same state and event use the last rule.
// Fixed permit targets are registered automatically. Dynamic redirect targets
// must have their own WithState declaration. Redirecting to the source state
// is a valid self-transition.
//
// Invalid programmer configuration, such as a nil option or nil callback,
// panics during construction. There is no runtime configuration API.
//
// # Guards, redirects, and effects
//
// Use WithGuard to reject a transition. Use WithPermitRedirect when the target
// depends on event data; a redirect to an undeclared state returns
// ErrUnknownState. Redirecting to the source state is a valid self-transition.
//
// OnTransition attaches an effect to one permit. OnEntry and OnExit attach
// effects to a state. Effects run after the new state is committed in this
// order: source exit effects, the permit's transition effect, and target entry
// effects. Multiple effects of one kind run in option order.
// Callback options should use the machine's D type. A non-nil event value with
// a mismatched callback data type panics when that callback runs.
//
// # Concurrency
//
// StateMachine is safe for concurrent use. Check serializes an event, its
// guard, and its redirect resolution without changing the state or running
// effects. Fire serializes a transition, its guard, and its effects.
// CurrentState and the inspection methods are safe to call concurrently and do
// not acquire the transition mutex.
// Check and a later Fire are separate operations, so the state may change
// between them. If validation and the state change must be atomic, call Fire
// and handle its returned error.
//
// Effects and guards run synchronously. Do not call Check or Fire from a guard
// or effect, because neither method is reentrant and callbacks run while the
// transition is locked. Keep long-running work in the surrounding event loop
// or actor runtime and submit a later event when that work completes.
//
// # Errors and inspection
//
// Check and Fire return the attempted Transition even when a transition is
// rejected. Errors wrap ErrInvalidEvent when the event is not configured for
// the current state, including when the state has no rules, ErrUnknownState
// when a redirect resolves to an undeclared state, or the guard's error when a
// guard rejects a transition. Use errors.Is to classify them.
package efsm
