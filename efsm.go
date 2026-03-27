package efsm

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrNoTransitions is returned by Fire when no transitions are defined for the
	// current state.
	ErrNoTransitions = errors.New("no transitions defined for state")

	// ErrInvalidEvent is returned by Fire when the supplied event is not valid in
	// the current state.
	ErrInvalidEvent = errors.New("event is not valid in current state")
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

// TransitionRule defines the outcome of an event, including the target state and an optional guard.
type TransitionRule[S comparable, E comparable, D any] struct {
	Target S
	Guard  Guard[S, E, D]
	Effect Effect[S, E, D]
}

// TransitionOption is a functional option type for configuring transition rules.
type TransitionOption[S comparable, E comparable, D any] func(*TransitionRule[S, E, D])

// WithGuard is a TransitionOption that sets a guard function for a transition rule.
// Note: If multiple WithGuard options are provided for the same transition, the last one overwrites previous ones.
func WithGuard[S comparable, E comparable, D any](guard Guard[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.Guard = guard
	}
}

// OnTransition is a TransitionOption that sets an effect function for a transition rule.
// Note: If multiple OnTransition options are provided for the same transition, the last one overwrites previous ones.
func OnTransition[S comparable, E comparable, D any](effect Effect[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.Effect = effect
	}
}

// StateBuilder provides a fluent API for configuring a specific state.
type StateBuilder[S comparable, E comparable, D any] struct {
	sm    *StateMachine[S, E, D]
	state S
}

// State begins the configuration of a specific state and returns a builder for method chaining.
func (sm *StateMachine[S, E, D]) State(state S) *StateBuilder[S, E, D] {
	return &StateBuilder[S, E, D]{
		sm:    sm,
		state: state,
	}
}

// OnEntry adds an effect that runs whenever this state is entered.
// Note: If called multiple times on the same builder, the last one overwrites previous ones.
func (sb *StateBuilder[S, E, D]) OnEntry(effect Effect[S, E, D]) *StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	data := sb.sm.states[sb.state]
	data.entryEffect = effect
	sb.sm.states[sb.state] = data

	return sb
}

// OnExit adds an effect that runs whenever this state is exited.
// Note: If called multiple times on the same builder, the last one overwrites previous ones.
func (sb *StateBuilder[S, E, D]) OnExit(effect Effect[S, E, D]) *StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	data := sb.sm.states[sb.state]
	data.exitEffect = effect
	sb.sm.states[sb.state] = data

	return sb
}

// Permit defines a valid transition triggered by an event, with optional transition options.
func (sb *StateBuilder[S, E, D]) Permit(event E, target S, opts ...TransitionOption[S, E, D]) *StateBuilder[S, E, D] {
	sb.sm.mutex.Lock()
	defer sb.sm.mutex.Unlock()

	rule := TransitionRule[S, E, D]{Target: target}
	for _, opt := range opts {
		opt(&rule)
	}

	data := sb.sm.states[sb.state]
	if data.transitions == nil {
		data.transitions = make(map[E]TransitionRule[S, E, D])
	}
	data.transitions[event] = rule
	sb.sm.states[sb.state] = data

	return sb
}

type stateData[S comparable, E comparable, D any] struct {
	transitions map[E]TransitionRule[S, E, D]
	entryEffect Effect[S, E, D]
	exitEffect  Effect[S, E, D]
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use: `State` and `AvailableEvents` use a read lock, and `Fire` uses
// a write lock to serialize transitions.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState S
	states       map[S]stateData[S, E, D]
	mutex        sync.RWMutex
}

// NewStateMachine creates a new StateMachine with the specified initial state.
func NewStateMachine[S comparable, E comparable, D any](initial S) *StateMachine[S, E, D] {
	return &StateMachine[S, E, D]{
		currentState: initial,
		states:       make(map[S]stateData[S, E, D]),
	}
}

// CurrentState returns the current state of the machine.
func (sm *StateMachine[S, E, D]) CurrentState() S {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.currentState
}

// AvailableStates returns a slice of states that have defined transitions.
func (sm *StateMachine[S, E, D]) AvailableStates() []S {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	states := make([]S, 0, len(sm.states))
	for state := range sm.states {
		states = append(states, state)
	}
	return states
}

// AvailableEvents returns a slice of events that are valid in the current state.
func (sm *StateMachine[S, E, D]) AvailableEvents() []E {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	data := sm.states[sm.currentState]
	events := make([]E, 0, len(data.transitions))
	for event := range data.transitions {
		events = append(events, event)
	}
	return events
}

// AvailableEventsForStates returns a map of states with their valid events.
func (sm *StateMachine[S, E, D]) AvailableEventsForStates() map[S][]E {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	eventsForStates := make(map[S][]E, len(sm.states))
	for state, data := range sm.states {
		events := make([]E, 0, len(data.transitions))
		for event := range data.transitions {
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
//   - Wrapped error if the guard fails.
func (sm *StateMachine[S, E, D]) Fire(event E, data D) error {
	sm.mutex.Lock()

	oldState := sm.currentState

	currentData, exists := sm.states[oldState]
	if !exists || currentData.transitions == nil {
		sm.mutex.Unlock()
		return fmt.Errorf("%w: %v", ErrNoTransitions, oldState)
	}

	rule, validEvent := currentData.transitions[event]
	if !validEvent {
		sm.mutex.Unlock()
		return fmt.Errorf("%w: %v in %v", ErrInvalidEvent, event, oldState)
	}

	transition := Transition[S, E]{From: oldState, To: rule.Target, Event: event}

	if rule.Guard != nil {
		if err := rule.Guard(transition, data); err != nil {
			sm.mutex.Unlock()
			return fmt.Errorf("transition guard failed: %w", err)
		}
	}

	sm.currentState = rule.Target

	targetData := sm.states[rule.Target]

	exitEffect := currentData.exitEffect
	transitionEffect := rule.Effect
	entryEffect := targetData.entryEffect

	sm.mutex.Unlock()

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
