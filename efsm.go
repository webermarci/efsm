package efsm

import (
	"context"
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
type Guard[S comparable, E comparable, D any] func(ctx context.Context, transition Transition[S, E], data D) error

// Effect defines a callback function executed after a state transition has occurred.
// It can be used for side effects like logging or triggering external actions.
type Effect[S comparable, E comparable, D any] func(ctx context.Context, transition Transition[S, E], data D)

// Rule defines the outcome of an event, including the target state and an optional guard.
type Rule[S comparable, E comparable, D any] struct {
	Target S
	Guard  Guard[S, E, D]
	Effect Effect[S, E, D]
}

// TransitionOption is a functional option type for configuring transition rules.
type TransitionOption[S comparable, E comparable, D any] func(*Rule[S, E, D])

// WithGuard is a TransitionOption that sets a guard function for a transition rule.
func WithGuard[S comparable, E comparable, D any](g Guard[S, E, D]) TransitionOption[S, E, D] {
	return func(r *Rule[S, E, D]) {
		r.Guard = g
	}
}

// WithEffect is a TransitionOption that sets an effect function for a transition rule.
func WithEffect[S comparable, E comparable, D any](e Effect[S, E, D]) TransitionOption[S, E, D] {
	return func(r *Rule[S, E, D]) {
		r.Effect = e
	}
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use: `State` and `AvailableEvents` use a read lock, and `Fire` uses
// a write lock to serialize transitions.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState S
	transitions  map[S]map[E]Rule[S, E, D]
	mutex        sync.RWMutex
}

// NewStateMachine creates a new StateMachine with the specified initial state.
func NewStateMachine[S comparable, E comparable, D any](initial S) *StateMachine[S, E, D] {
	return &StateMachine[S, E, D]{
		currentState: initial,
		transitions:  make(map[S]map[E]Rule[S, E, D]),
	}
}

// Permit defines a transition from a state to a target state triggered by an event, with optional transition options.
func (sm *StateMachine[S, E, D]) Permit(state S, event E, target S, options ...TransitionOption[S, E, D]) *StateMachine[S, E, D] {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if _, exists := sm.transitions[state]; !exists {
		sm.transitions[state] = make(map[E]Rule[S, E, D])
	}

	rule := Rule[S, E, D]{Target: target}

	for _, option := range options {
		option(&rule)
	}

	sm.transitions[state][event] = rule

	return sm
}

// State returns the current state of the machine.
func (sm *StateMachine[S, E, D]) State() S {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.currentState
}

// AvailableStates returns a slice of states that have defined transitions.
func (sm *StateMachine[S, E, D]) AvailableStates() []S {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var states []S
	for state := range sm.transitions {
		states = append(states, state)
	}
	return states
}

// AvailableEvents returns a slice of events that are valid in the current state.
func (sm *StateMachine[S, E, D]) AvailableEvents() []E {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var events []E
	if stateRules, exists := sm.transitions[sm.currentState]; exists {
		for event := range stateRules {
			events = append(events, event)
		}
	}
	return events
}

// AvailableEventsForStates returns a map of states with their valid events.
func (sm *StateMachine[S, E, D]) AvailableEventsForStates() map[S][]E {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	eventsForStates := make(map[S][]E)
	for state, stateRules := range sm.transitions {
		var events []E
		for event := range stateRules {
			events = append(events, event)
		}
		eventsForStates[state] = events
	}
	return eventsForStates
}

// Fire attempts to transition the state machine using the provided event.
// If the matching Rule defines a Guard, it is executed synchronously. If the
// guard returns an error the transition does not occur and the error is returned.
//
// Returns:
//   - ErrNoTransitions if no transitions are defined for the current state.
//   - ErrInvalidEvent if the supplied event is not valid for the current state.
//   - Wrapped error if the guard fails.
func (sm *StateMachine[S, E, D]) Fire(ctx context.Context, event E, data D) error {
	sm.mutex.Lock()

	oldState := sm.currentState

	stateRules, exists := sm.transitions[oldState]
	if !exists {
		sm.mutex.Unlock()
		return fmt.Errorf("%w: %v", ErrNoTransitions, oldState)
	}

	rule, validEvent := stateRules[event]
	if !validEvent {
		sm.mutex.Unlock()
		return fmt.Errorf("%w: %v in %v", ErrInvalidEvent, event, oldState)
	}

	transition := Transition[S, E]{From: oldState, To: rule.Target, Event: event}

	if rule.Guard != nil {
		if err := rule.Guard(ctx, transition, data); err != nil {
			sm.mutex.Unlock()
			return fmt.Errorf("transition guard failed: %w", err)
		}
	}

	sm.currentState = rule.Target

	effect := rule.Effect
	sm.mutex.Unlock()

	if effect != nil {
		effect(ctx, transition, data)
	}

	return nil
}
