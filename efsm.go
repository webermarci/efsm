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

// Action defines a callback function executed during a state transition.
type Action[S comparable, E comparable, D any] func(ctx context.Context, transition Transition[S, E], data D) error

// Rule defines the outcome of an event, including the target state and an optional action.
type Rule[S comparable, E comparable, D any] struct {
	Target S
	Action Action[S, E, D]
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use: `State` and `AvailableEvents` use a read lock, and `Fire` uses
// a write lock to serialize transitions.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState S
	transitions  map[S]map[E]Rule[S, E, D]
	mutex        sync.RWMutex
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

// AvaliableEventsForStates returns a map of states with their valid events.
func (sm *StateMachine[S, E, D]) AvaliableEventsForStates() map[S][]E {
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
// If the matching Rule defines an Action, it is executed synchronously. If the
// action returns an error the transition does not occur and the error is returned.
//
// Returns:
//   - ErrNoTransitions if no transitions are defined for the current state.
//   - ErrInvalidEvent if the supplied event is not valid for the current state.
//   - Wrapped error if the action fails.
func (sm *StateMachine[S, E, D]) Fire(ctx context.Context, event E, data D) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.currentState

	stateRules, exists := sm.transitions[oldState]
	if !exists {
		return fmt.Errorf("%w: %v", ErrNoTransitions, oldState)
	}

	rule, validEvent := stateRules[event]
	if !validEvent {
		return fmt.Errorf("%w: %v in %v", ErrInvalidEvent, event, oldState)
	}

	if rule.Action != nil {
		transition := Transition[S, E]{From: oldState, To: rule.Target, Event: event}
		if err := rule.Action(ctx, transition, data); err != nil {
			return fmt.Errorf("transition action failed: %w", err)
		}
	}

	sm.currentState = rule.Target
	return nil
}

// Builder helps configure the state machine before it is built.
type Builder[S comparable, E comparable, D any] struct {
	initialState S
	transitions  map[S]map[E]Rule[S, E, D]
}

// NewBuilder returns a new Builder with the given initial state.
func NewBuilder[S comparable, E comparable, D any](initial S) *Builder[S, E, D] {
	return &Builder[S, E, D]{
		initialState: initial,
		transitions:  make(map[S]map[E]Rule[S, E, D]),
	}
}

// Configure prepares the builder to accept transition rules for `state` and
// returns a `StateBuilder` for fluently declaring rules.
func (b *Builder[S, E, D]) Configure(state S) *StateBuilder[S, E, D] {
	if _, exists := b.transitions[state]; !exists {
		b.transitions[state] = make(map[E]Rule[S, E, D])
	}
	return &StateBuilder[S, E, D]{
		builder: b,
		state:   state,
	}
}

// Build finalizes the configuration and returns a `StateMachine`.
func (b *Builder[S, E, D]) Build() *StateMachine[S, E, D] {
	return &StateMachine[S, E, D]{
		currentState: b.initialState,
		transitions:  b.transitions,
	}
}

// StateBuilder configures rules for a single state.
type StateBuilder[S comparable, E comparable, D any] struct {
	builder *Builder[S, E, D]
	state   S
}

// Permit adds a transition for `event` that targets `target` without an action.
func (sb *StateBuilder[S, E, D]) Permit(event E, target S) *StateBuilder[S, E, D] {
	sb.builder.transitions[sb.state][event] = Rule[S, E, D]{Target: target}
	return sb
}

// PermitWithAction adds a transition for `event` that targets `target` and
// executes `action` when the event is fired.
func (sb *StateBuilder[S, E, D]) PermitWithAction(event E, target S, action Action[S, E, D]) *StateBuilder[S, E, D] {
	sb.builder.transitions[sb.state][event] = Rule[S, E, D]{Target: target, Action: action}
	return sb
}
