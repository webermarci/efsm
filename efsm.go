package efsm

import (
	"errors"
	"sync"
	"sync/atomic"
)

var (
	// ErrNoTransitions is returned by Fire when no transitions are defined for the
	// current state.
	ErrNoTransitions = errors.New("no transitions defined for state")

	// ErrInvalidEvent is returned by Fire when the supplied event is not valid in
	// the current state.
	ErrInvalidEvent = errors.New("event is not valid in current state")

	// ErrSelfRedirect is returned by builder methods when a transition is configured
	// to redirect to the source state.
	ErrSelfRedirect = errors.New("transition cannot redirect to the source state")
)

// Transition represents a state change triggered by an event.
type Transition[S comparable, E comparable] struct {
	From  S
	To    S
	Event E
}

// Guard defines a callback function executed during a state transition.
type Guard[S comparable, E comparable, D any] func(transition Transition[S, E], data D) error

// Effect defines a callback function executed after a state transition has occurred.
// It can be used for side effects like logging or triggering external actions.
type Effect[S comparable, E comparable, D any] func(transition Transition[S, E], data D)

// Redirect defines a callback function that can dynamically determine the target state during a transition.
// It is executed during the transition process and can be used to implement dynamic state transitions based on runtime conditions.
type Redirect[S comparable, E comparable, D any] func(transition Transition[S, E], data D) S

// TransitionRule defines the outcome of an event, including the target state and an optional guard.
type TransitionRule[S comparable, E comparable, D any] struct {
	target      S
	boxedTarget any
	targetNode  *stateData[S, E, D]
	guard       Guard[S, E, D]
	effect      Effect[S, E, D]
	redirect    Redirect[S, E, D]
}

// TransitionOption is a functional option type for configuring transition rules.
type TransitionOption[S comparable, E comparable, D any] func(*TransitionRule[S, E, D])

// WithGuard is a TransitionOption that sets a guard function for a transition rule.
// If multiple WithGuard options are provided for the same transition, the last one overwrites previous ones.
//
// IMPORTANT: Guards are called while the state machine lock is held. You MUST NOT
// call Fire() synchronously from within a guard — doing so will deadlock.
// Guards should only validate conditions and return an error to block the transition.
func WithGuard[S comparable, E comparable, D any](guard Guard[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.guard = guard
	}
}

// WithRedirect is a TransitionOption that sets a redirect function for a transition rule.
// Redirecting to the source state is not allowed and will result in an `ErrSelfRedirect`.
// If multiple WithRedirect options are provided for the same transition, the last one overwrites previous ones.
//
// IMPORTANT: Redirects are called while the state machine lock is held. You MUST NOT
// call Fire() synchronously from within a redirect — doing so will deadlock.
// Redirects should only validate conditions and return a state.
func WithRedirect[S comparable, E comparable, D any](redirect Redirect[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.redirect = redirect
	}
}

// OnTransition is a TransitionOption that sets an effect function for a transition rule.
// If multiple OnTransition options are provided for the same transition, the last one overwrites previous ones.
//
// IMPORTANT: Effects are called while the state machine lock is held. You MUST NOT
// call Fire() synchronously from within an effect — doing so will deadlock.
// To trigger a subsequent transition from an effect, spawn a new goroutine.
func OnTransition[S comparable, E comparable, D any](effect Effect[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.effect = effect
	}
}

// StateBuilder provides a fluent API for configuring a specific state.
type StateBuilder[S comparable, E comparable, D any] struct {
	sm    *StateMachine[S, E, D]
	state S
}

// State begins the configuration of a specific state and returns a builder for method chaining.
func (sm *StateMachine[S, E, D]) State(state S) StateBuilder[S, E, D] {
	return StateBuilder[S, E, D]{
		sm:    sm,
		state: state,
	}
}

// OnEntry adds an effect that runs whenever this state is entered.
// If called multiple times on the same builder, the last one overwrites previous ones.
//
// IMPORTANT: Effects are called while the state machine lock is held. You MUST NOT
// call Fire() synchronously from within an effect — doing so will deadlock.
// To trigger a subsequent transition from an effect, spawn a new goroutine.
func (sb StateBuilder[S, E, D]) OnEntry(effect Effect[S, E, D]) StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	node := sb.sm.getOrCreateNode(sb.state)
	node.entryEffect = effect
	return sb
}

// OnExit adds an effect that runs whenever this state is exited.
// If called multiple times on the same builder, the last one overwrites previous ones.
//
// IMPORTANT: Effects are called while the state machine lock is held. You MUST NOT
// call Fire() synchronously from within an effect — doing so will deadlock.
// To trigger a subsequent transition from an effect, spawn a new goroutine.
func (sb StateBuilder[S, E, D]) OnExit(effect Effect[S, E, D]) StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	node := sb.sm.getOrCreateNode(sb.state)
	node.exitEffect = effect
	return sb
}

// Permit defines a valid transition triggered by an event, with optional transition options.
func (sb StateBuilder[S, E, D]) Permit(event E, target S, opts ...TransitionOption[S, E, D]) StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	targetNode := sb.sm.getOrCreateNode(target)

	rule := TransitionRule[S, E, D]{
		target:      target,
		boxedTarget: any(target),
		targetNode:  targetNode,
	}

	for _, opt := range opts {
		opt(&rule)
	}

	sourceNode := sb.sm.getOrCreateNode(sb.state)
	if sourceNode.transitions == nil {
		sourceNode.transitions = make(map[E]TransitionRule[S, E, D])
	}
	sourceNode.transitions[event] = rule

	return sb
}

type stateData[S comparable, E comparable, D any] struct {
	transitions map[E]TransitionRule[S, E, D]
	entryEffect Effect[S, E, D]
	exitEffect  Effect[S, E, D]
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use. CurrentState provides 100% lock-free reads, while Fire and
// builder methods use a mutex to safely serialize transitions and map mutations.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState atomic.Value
	_            [64]byte

	mutex      sync.Mutex
	innerState S
	active     *stateData[S, E, D]

	states map[S]*stateData[S, E, D]
}

// NewStateMachine creates a new StateMachine with the specified initial state.
func NewStateMachine[S comparable, E comparable, D any](initial S) *StateMachine[S, E, D] {
	boxedInitial := any(initial)
	sm := &StateMachine[S, E, D]{
		innerState: initial,
		states:     make(map[S]*stateData[S, E, D]),
	}
	sm.active = sm.getOrCreateNode(initial)
	sm.currentState.Store(boxedInitial)
	return sm
}

func (sm *StateMachine[S, E, D]) getOrCreateNode(state S) *stateData[S, E, D] {
	node, exists := sm.states[state]
	if !exists {
		node = &stateData[S, E, D]{}
		sm.states[state] = node
	}
	return node
}

// CurrentState returns the current state of the machine.
//
// This is a lock-free read. The returned state may reflect a completed
// transition whose effects (OnEntry, OnExit, OnTransition) are still executing
// on another goroutine.
func (sm *StateMachine[S, E, D]) CurrentState() S {
	return sm.currentState.Load().(S)
}

// AvailableStates returns a slice of all registered states,
// including terminal states with no outgoing transitions.
func (sm *StateMachine[S, E, D]) AvailableStates() []S {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	var states []S
	for state := range sm.states {
		states = append(states, state)
	}
	return states
}

// AvailableEvents returns a slice of events that are valid in the current state.
func (sm *StateMachine[S, E, D]) AvailableEvents() []E {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	data := sm.active

	events := make([]E, 0, len(data.transitions))
	for event := range data.transitions {
		events = append(events, event)
	}
	return events
}

// AvailableEventsForStates returns a map of states with their valid events.
func (sm *StateMachine[S, E, D]) AvailableEventsForStates() map[S][]E {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	eventsForStates := make(map[S][]E)
	for state, node := range sm.states {
		if len(node.transitions) == 0 {
			continue
		}

		events := make([]E, 0, len(node.transitions))
		for event := range node.transitions {
			events = append(events, event)
		}
		eventsForStates[state] = events
	}
	return eventsForStates
}

// Fire attempts to transition the state machine using the provided event.
// If the matching TransitionRule defines a Guard, it is executed synchronously.
// If the guard returns an error the transition does not occur and the error is returned.
//
// Returns:
//   - ErrNoTransitions if no transitions are defined for the current state.
//   - ErrInvalidEvent if the supplied event is not valid for the current state.
//   - Guard error if the guard fails.
func (sm *StateMachine[S, E, D]) Fire(event E, data D) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	currentNode := sm.active

	if currentNode.transitions == nil {
		return ErrNoTransitions
	}

	rule, validEvent := currentNode.transitions[event]
	if !validEvent {
		return ErrInvalidEvent
	}

	transition := Transition[S, E]{From: sm.innerState, To: rule.target, Event: event}

	if rule.guard != nil {
		if err := rule.guard(transition, data); err != nil {
			return err
		}
	}

	if rule.redirect != nil {
		redirectTarget := rule.redirect(transition, data)

		if redirectTarget == transition.From {
			return ErrSelfRedirect
		}

		rule.target = redirectTarget
		rule.boxedTarget = any(redirectTarget)
		rule.targetNode = sm.getOrCreateNode(redirectTarget)

		transition.To = redirectTarget
	}

	sm.innerState = rule.target
	sm.currentState.Store(rule.boxedTarget)

	sm.active = rule.targetNode

	exitEffect := currentNode.exitEffect
	transitionEffect := rule.effect
	entryEffect := rule.targetNode.entryEffect

	if exitEffect != nil {
		exitEffect(transition, data)
	}

	if transitionEffect != nil {
		transitionEffect(transition, data)
	}

	if entryEffect != nil {
		entryEffect(transition, data)
	}

	return nil
}
