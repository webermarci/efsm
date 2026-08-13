package efsm

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
)

var (
	// ErrInvalidEvent is wrapped by Fire when the supplied event is not valid in
	// the current state. Use errors.Is to classify the error.
	ErrInvalidEvent = errors.New("event is not valid in current state")

	// ErrUnknownState is wrapped by Fire when a dynamic redirect resolves to a
	// state that was not declared with WithState.
	ErrUnknownState = errors.New("redirect resolved to unknown state")
)

// Transition represents a state change triggered by an event.
type Transition[S comparable, E comparable] struct {
	From  S
	To    S
	Event E
}

// Guard defines a callback that can reject a state transition.
type Guard[S comparable, E comparable, D any] func(t Transition[S, E], data D) error

// Effect defines a callback executed as part of a state transition.
type Effect[S comparable, E comparable, D any] func(t Transition[S, E], data D)

// Redirect defines a callback that dynamically determines a transition target.
// The returned state must be declared with WithState.
type Redirect[S comparable, E comparable, D any] func(t Transition[S, E], data D) S

type transitionDefinition[S comparable, E comparable] struct {
	target   *stateDefinition[S, E]
	guard    func(Transition[S, E], any) error
	effect   func(Transition[S, E], any)
	redirect func(Transition[S, E], any) S
}

type stateDefinition[S comparable, E comparable] struct {
	machine      *machineBuilder[S, E]
	state        S
	declared     bool
	transitions  map[E]transitionDefinition[S, E]
	eventOrder   []E
	entryEffects []func(Transition[S, E], any)
	exitEffects  []func(Transition[S, E], any)
}

type machineBuilder[S comparable, E comparable] struct {
	states     map[S]*stateDefinition[S, E]
	stateOrder []S
}

func (b *machineBuilder[S, E]) getOrCreate(state S) *stateDefinition[S, E] {
	definition, exists := b.states[state]
	if exists {
		return definition
	}

	definition = &stateDefinition[S, E]{
		machine: b,
		state:   state,
	}
	b.states[state] = definition
	b.stateOrder = append(b.stateOrder, state)
	return definition
}

// StateOption configures one state during NewStateMachine. Values returned by
// OnEntry, OnExit, WithPermit, and WithPermitRedirect can be stored and reused
// across multiple WithState options.
type StateOption[S comparable, E comparable] func(*stateDefinition[S, E])

// TransitionOption configures one permit during NewStateMachine. Values
// returned by WithGuard and OnTransition can be stored and reused.
type TransitionOption[S comparable, E comparable] func(*transitionDefinition[S, E])

// StateMachineOption configures a StateMachine during construction.
type StateMachineOption[S comparable, E comparable] func(*machineBuilder[S, E])

// WithState groups the options belonging to one state. The graph is frozen
// when NewStateMachine returns; there is no runtime configuration API.
func WithState[S comparable, E comparable](state S, options ...StateOption[S, E]) StateMachineOption[S, E] {
	return func(builder *machineBuilder[S, E]) {
		definition := builder.getOrCreate(state)
		definition.declared = true

		for _, option := range options {
			if option == nil {
				panic("efsm: nil state option")
			}
			option(definition)
		}
	}
}

// WithPermit declares an event transition from the state passed to WithState.
// If the same event is permitted more than once for a state, the last permit
// replaces the previous rule.
func WithPermit[S comparable, E comparable](event E, target S, options ...TransitionOption[S, E]) StateOption[S, E] {
	return func(definition *stateDefinition[S, E]) {
		rule := transitionDefinition[S, E]{target: definition.machine.getOrCreate(target)}
		for _, option := range options {
			if option == nil {
				panic("efsm: nil transition option")
			}
			option(&rule)
		}

		// Fixed targets are registered automatically. They may be left without
		// their own WithState option when they have no behavior of their own.
		if definition.transitions == nil {
			definition.transitions = make(map[E]transitionDefinition[S, E])
		}
		if _, exists := definition.transitions[event]; !exists {
			definition.eventOrder = append(definition.eventOrder, event)
		}
		definition.transitions[event] = rule
	}
}

// WithPermitRedirect declares an event transition whose target is resolved at
// runtime. The resolved target must be declared with WithState.
func WithPermitRedirect[S comparable, E comparable, D any](event E, redirect Redirect[S, E, D], options ...TransitionOption[S, E]) StateOption[S, E] {
	if redirect == nil {
		panic(fmt.Sprintf("efsm: nil redirect for event %v", event))
	}

	return func(definition *stateDefinition[S, E]) {
		rule := transitionDefinition[S, E]{
			target: definition,
			redirect: func(t Transition[S, E], data any) S {
				return redirect(t, castData[D](data))
			},
		}
		for _, option := range options {
			if option == nil {
				panic("efsm: nil transition option")
			}
			option(&rule)
		}

		if definition.transitions == nil {
			definition.transitions = make(map[E]transitionDefinition[S, E])
		}
		if _, exists := definition.transitions[event]; !exists {
			definition.eventOrder = append(definition.eventOrder, event)
		}
		definition.transitions[event] = rule
	}
}

// OnEntry adds an effect that runs whenever this state is entered. Effects run
// in the order their options are passed to WithState.
func OnEntry[S comparable, E comparable, D any](effect Effect[S, E, D]) StateOption[S, E] {
	if effect == nil {
		panic("efsm: nil entry effect")
	}

	return func(definition *stateDefinition[S, E]) {
		definition.entryEffects = append(definition.entryEffects, func(t Transition[S, E], data any) {
			effect(t, castData[D](data))
		})
	}
}

// OnExit adds an effect that runs whenever this state is exited. Effects run
// in the order their options are passed to WithState.
func OnExit[S comparable, E comparable, D any](effect Effect[S, E, D]) StateOption[S, E] {
	if effect == nil {
		panic("efsm: nil exit effect")
	}

	return func(definition *stateDefinition[S, E]) {
		definition.exitEffects = append(definition.exitEffects, func(t Transition[S, E], data any) {
			effect(t, castData[D](data))
		})
	}
}

// WithGuard rejects a permit when the guard returns an error.
func WithGuard[S comparable, E comparable, D any](guard Guard[S, E, D]) TransitionOption[S, E] {
	if guard == nil {
		panic("efsm: nil guard")
	}

	return func(rule *transitionDefinition[S, E]) {
		rule.guard = func(t Transition[S, E], data any) error {
			return guard(t, castData[D](data))
		}
	}
}

// OnTransition adds an effect that runs for one permit after the new state is
// committed and before the target state's entry effects.
func OnTransition[S comparable, E comparable, D any](effect Effect[S, E, D]) TransitionOption[S, E] {
	if effect == nil {
		panic("efsm: nil transition effect")
	}

	return func(rule *transitionDefinition[S, E]) {
		rule.effect = func(t Transition[S, E], data any) {
			effect(t, castData[D](data))
		}
	}
}

func castData[D any](data any) D {
	if data == nil {
		var zero D
		return zero
	}
	value, ok := data.(D)
	if !ok {
		panic(fmt.Sprintf("efsm: event data has type %T, want %T", data, *new(D)))
	}
	return value
}

// StateMachine is an immutable state graph with a safely synchronized current
// state. Construct it with NewStateMachine; it cannot be reconfigured after
// construction.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState atomic.Pointer[stateDefinition[S, E]]
	mutex        sync.Mutex
	states       map[S]*stateDefinition[S, E]
	stateOrder   []S
}

// NewStateMachine creates a state machine with an immutable configuration.
// The initial state is declared automatically. Fixed permit targets are
// registered automatically; dynamic redirect targets must be declared with
// WithState.
func NewStateMachine[S comparable, E comparable, D any](initial S, options ...StateMachineOption[S, E]) *StateMachine[S, E, D] {
	builder := &machineBuilder[S, E]{
		states: make(map[S]*stateDefinition[S, E]),
	}
	builder.getOrCreate(initial).declared = true

	for _, option := range options {
		if option == nil {
			panic("efsm: nil state machine option")
		}
		option(builder)
	}

	sm := &StateMachine[S, E, D]{
		states:     builder.states,
		stateOrder: slices.Clone(builder.stateOrder),
	}
	sm.currentState.Store(builder.states[initial])
	return sm
}

// CurrentState returns the current state of the machine.
func (sm *StateMachine[S, E, D]) CurrentState() S {
	return sm.currentState.Load().state
}

// AvailableStates returns all registered states in registration order.
func (sm *StateMachine[S, E, D]) AvailableStates() []S {
	return slices.Clone(sm.stateOrder)
}

// AvailableEvents returns the events valid in the current state in registration
// order.
func (sm *StateMachine[S, E, D]) AvailableEvents() []E {
	return slices.Clone(sm.currentState.Load().eventOrder)
}

// AvailableEventsForStates returns the valid events for each registered state
// that has at least one event, in registration order. The returned map and
// slices are copies. Map key iteration remains subject to Go's map semantics.
func (sm *StateMachine[S, E, D]) AvailableEventsForStates() map[S][]E {
	eventsForStates := make(map[S][]E)
	for _, state := range sm.stateOrder {
		node := sm.states[state]
		if len(node.eventOrder) == 0 {
			continue
		}
		eventsForStates[state] = slices.Clone(node.eventOrder)
	}
	return eventsForStates
}

// Fire attempts to transition the state machine using the provided event. It
// serializes the transition, guard, and effects. The attempted transition is
// returned even when the transition is rejected.
func (sm *StateMachine[S, E, D]) Fire(event E, data D) (Transition[S, E], error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	currentNode := sm.currentState.Load()
	transition := Transition[S, E]{
		From:  currentNode.state,
		To:    currentNode.state,
		Event: event,
	}

	rule, validEvent := currentNode.transitions[event]
	if !validEvent {
		return transition, fmt.Errorf("%w (event %v, state %v)", ErrInvalidEvent, event, currentNode.state)
	}

	transition.To = rule.target.state
	if rule.guard != nil {
		if err := rule.guard(transition, data); err != nil {
			return transition, fmt.Errorf("guard rejected event %v in state %v: %w", event, currentNode.state, err)
		}
	}

	targetNode := rule.target
	if rule.redirect != nil {
		redirectTarget := rule.redirect(transition, data)
		redirectedNode, exists := sm.states[redirectTarget]
		if !exists || !redirectedNode.declared {
			transition.To = redirectTarget
			return transition, fmt.Errorf("%w (event %v, state %v, target %v)", ErrUnknownState, event, transition.From, redirectTarget)
		}
		targetNode = redirectedNode
		transition.To = redirectTarget
	}

	sm.currentState.Store(targetNode)

	for _, effect := range currentNode.exitEffects {
		effect(transition, data)
	}
	if rule.effect != nil {
		rule.effect(transition, data)
	}
	for _, effect := range targetNode.entryEffects {
		effect(transition, data)
	}

	return transition, nil
}
