package efsm

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrNoTransitions = errors.New("no transitions defined for state")
	ErrInvalidEvent  = errors.New("event is not valid in current state")
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

type StateMachine[S comparable, E comparable, D any] struct {
	currentState S
	transitions  map[S]map[E]Rule[S, E, D]
	mutex        sync.RWMutex
}

func (sm *StateMachine[S, E, D]) State() S {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.currentState
}

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

// Fire attempts to transition the state machine using the provided event.
// If the transition defines an Action, it will be executed synchronously.
// Returns an error if the transition is invalid or if the Action fails.
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

type Builder[S comparable, E comparable, D any] struct {
	initialState S
	transitions  map[S]map[E]Rule[S, E, D]
}

func NewBuilder[S comparable, E comparable, D any](initial S) *Builder[S, E, D] {
	return &Builder[S, E, D]{
		initialState: initial,
		transitions:  make(map[S]map[E]Rule[S, E, D]),
	}
}

func (b *Builder[S, E, D]) Configure(state S) *StateBuilder[S, E, D] {
	if _, exists := b.transitions[state]; !exists {
		b.transitions[state] = make(map[E]Rule[S, E, D])
	}
	return &StateBuilder[S, E, D]{
		builder: b,
		state:   state,
	}
}

func (b *Builder[S, E, D]) Build() *StateMachine[S, E, D] {
	return &StateMachine[S, E, D]{
		currentState: b.initialState,
		transitions:  b.transitions,
	}
}

type StateBuilder[S comparable, E comparable, D any] struct {
	builder *Builder[S, E, D]
	state   S
}

func (sb *StateBuilder[S, E, D]) Permit(event E, target S) *StateBuilder[S, E, D] {
	sb.builder.transitions[sb.state][event] = Rule[S, E, D]{Target: target}
	return sb
}

func (sb *StateBuilder[S, E, D]) PermitWithAction(event E, target S, action Action[S, E, D]) *StateBuilder[S, E, D] {
	sb.builder.transitions[sb.state][event] = Rule[S, E, D]{Target: target, Action: action}
	return sb
}
