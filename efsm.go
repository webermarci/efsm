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
type Guard[S comparable, E comparable, D any] func(t Transition[S, E], data D) error

// Effect defines a callback function executed after a state transition has occurred.
type Effect[S comparable, E comparable, D any] func(t Transition[S, E], data D)

// Redirect defines a callback function that can dynamically determine the target state during a transition.
type Redirect[S comparable, E comparable, D any] func(t Transition[S, E], data D) S

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
func WithGuard[S comparable, E comparable, D any](guard Guard[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.guard = guard
	}
}

func withRedirect[S comparable, E comparable, D any](redirect Redirect[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.redirect = redirect
	}
}

// OnTransition is a TransitionOption that sets an effect function for a transition rule.
func OnTransition[S comparable, E comparable, D any](effect Effect[S, E, D]) TransitionOption[S, E, D] {
	return func(rule *TransitionRule[S, E, D]) {
		rule.effect = effect
	}
}

// StateConfigurator provides a closure-based API for configuring a specific state.
// It assumes the caller holds the state machine lock, preventing lock thrashing.
type StateConfigurator[S comparable, E comparable, D any] struct {
	sm    *StateMachine[S, E, D]
	state S
}

// OnEntry adds an effect that runs whenever this state is entered.
// If called multiple times (e.g., via mixins), the effects are executed in the order they were added.
func (c *StateConfigurator[S, E, D]) OnEntry(effect Effect[S, E, D]) *StateConfigurator[S, E, D] {
	if effect == nil {
		return c
	}
	node := c.sm.getOrCreateNode(c.state)
	node.entryEffect = append(node.entryEffect, effect)
	return c
}

// OnExit adds an effect that runs whenever this state is exited.
// If called multiple times (e.g., via mixins), the effects are executed in the order they were added.
func (c *StateConfigurator[S, E, D]) OnExit(effect Effect[S, E, D]) *StateConfigurator[S, E, D] {
	if effect == nil {
		return c
	}
	node := c.sm.getOrCreateNode(c.state)
	node.exitEffect = append(node.exitEffect, effect)
	return c
}

// Permit defines a valid transition triggered by an event.
func (c *StateConfigurator[S, E, D]) Permit(event E, target S, opts ...TransitionOption[S, E, D]) *StateConfigurator[S, E, D] {
	targetNode := c.sm.getOrCreateNode(target)

	rule := TransitionRule[S, E, D]{
		target:      target,
		boxedTarget: any(target),
		targetNode:  targetNode,
	}

	for _, opt := range opts {
		opt(&rule)
	}

	sourceNode := c.sm.getOrCreateNode(c.state)
	if sourceNode.transitions == nil {
		sourceNode.transitions = make(map[E]TransitionRule[S, E, D])
	}
	sourceNode.transitions[event] = rule

	return c
}

// PermitRedirect defines a transition whose target state is determined dynamically at runtime.
func (c *StateConfigurator[S, E, D]) PermitRedirect(event E, redirect Redirect[S, E, D], opts ...TransitionOption[S, E, D]) *StateConfigurator[S, E, D] {
	allOpts := append([]TransitionOption[S, E, D]{withRedirect(redirect)}, opts...)
	return c.Permit(event, c.state, allOpts...)
}

type stateData[S comparable, E comparable, D any] struct {
	transitions map[E]TransitionRule[S, E, D]
	entryEffect []Effect[S, E, D]
	exitEffect  []Effect[S, E, D]
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use. CurrentState provides 100% lock-free reads.
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

// Configure locks the state machine once and applies all provided setup functions.
// If no setup functions are provided, it simply registers the state.
func (sm *StateMachine[S, E, D]) Configure(state S, setups ...func(c *StateConfigurator[S, E, D])) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.getOrCreateNode(state)

	if len(setups) == 0 {
		return
	}

	c := &StateConfigurator[S, E, D]{
		sm:    sm,
		state: state,
	}

	for _, setup := range setups {
		if setup != nil {
			setup(c)
		}
	}
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
func (sm *StateMachine[S, E, D]) CurrentState() S {
	return sm.currentState.Load().(S)
}

// AvailableStates returns a slice of all registered states.
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

// CanFire returns true if the event is registered for the current state.
// Note: This only checks if the transition exists. If the transition has a Guard
// that would fail, Fire() will still return an error.
func (sm *StateMachine[S, E, D]) CanFire(event E) bool {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.active == nil || sm.active.transitions == nil {
		return false
	}
	_, exists := sm.active.transitions[event]
	return exists
}

// MustFire attempts to transition the state machine and panics if it fails.
// This is useful for programmatic transitions where an invalid event is considered
// a fatal developer error, eliminating the need for boilerplate error handling.
func (sm *StateMachine[S, E, D]) MustFire(event E, data D) {
	if err := sm.Fire(event, data); err != nil {
		panic("efsm: MustFire failed: " + err.Error())
	}
}

// Fire attempts to transition the state machine using the provided event.
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

	exitEffects := currentNode.exitEffect
	transitionEffect := rule.effect
	entryEffects := rule.targetNode.entryEffect

	for _, effect := range exitEffects {
		effect(transition, data)
	}

	if transitionEffect != nil {
		transitionEffect(transition, data)
	}

	for _, effect := range entryEffects {
		effect(transition, data)
	}

	return nil
}
