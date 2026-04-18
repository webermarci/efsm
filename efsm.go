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

// Observer provides a set of callbacks that can be used to observe state machine behavior.
type Observer[S comparable, E comparable, D any] struct {
	OnTransitioning func(t Transition[S, E], data D)
	OnRedirected    func(t Transition[S, E], newTarget S, data D)
	OnGuardFiltered func(t Transition[S, E], err error, data D)
	OnTransitioned  func(t Transition[S, E], data D)
	OnInvalidEvent  func(t Transition[S, E], data D)
}

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
	target     S
	targetNode *stateData[S, E, D]
	guard      Guard[S, E, D]
	effect     Effect[S, E, D]
	redirect   Redirect[S, E, D]
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
		target:     target,
		targetNode: targetNode,
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
	boxedState  any
	transitions map[E]TransitionRule[S, E, D]
	entryEffect []Effect[S, E, D]
	exitEffect  []Effect[S, E, D]
}

// StateMachineOption is a functional option type for configuring the state machine.
type StateMachineOption[S comparable, E comparable, D any] func(*StateMachine[S, E, D])

// WithObserver is a StateMachineOption that sets an observer for the state machine.
func WithObserver[S comparable, E comparable, D any](observer *Observer[S, E, D]) StateMachineOption[S, E, D] {
	return func(sm *StateMachine[S, E, D]) {
		sm.observer = observer
	}
}

// StateMachine is a running finite state machine instance. It is safe for
// concurrent use. CurrentState provides 100% lock-free reads.
type StateMachine[S comparable, E comparable, D any] struct {
	currentState atomic.Value
	_            [64]byte
	mutex        sync.RWMutex
	innerState   S
	active       *stateData[S, E, D]
	states       map[S]*stateData[S, E, D]
	observer     *Observer[S, E, D]
}

// NewStateMachine creates a new StateMachine with the specified initial state and optional configuration options. The initial state is registered automatically.
func NewStateMachine[S comparable, E comparable, D any](initial S, opts ...StateMachineOption[S, E, D]) *StateMachine[S, E, D] {
	boxedInitial := any(initial)
	sm := &StateMachine[S, E, D]{
		innerState: initial,
		states:     make(map[S]*stateData[S, E, D]),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(sm)
		}
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
		node = &stateData[S, E, D]{
			boxedState: any(state),
		}
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
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var states []S
	for state := range sm.states {
		states = append(states, state)
	}
	return states
}

// AvailableEvents returns a slice of events that are valid in the current state.
func (sm *StateMachine[S, E, D]) AvailableEvents() []E {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	data := sm.active

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
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

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

	transition := Transition[S, E]{
		From:  sm.innerState,
		To:    sm.innerState,
		Event: event,
	}

	if currentNode.transitions == nil {
		if sm.observer != nil && sm.observer.OnInvalidEvent != nil {
			sm.observer.OnInvalidEvent(transition, data)
		}
		return ErrNoTransitions
	}

	rule, validEvent := currentNode.transitions[event]
	if !validEvent {
		if sm.observer != nil && sm.observer.OnInvalidEvent != nil {
			sm.observer.OnInvalidEvent(transition, data)
		}
		return ErrInvalidEvent
	}

	transition.To = rule.target

	if sm.observer != nil && sm.observer.OnTransitioning != nil {
		sm.observer.OnTransitioning(transition, data)
	}

	if rule.guard != nil {
		if err := rule.guard(transition, data); err != nil {
			if sm.observer != nil && sm.observer.OnGuardFiltered != nil {
				sm.observer.OnGuardFiltered(transition, err, data)
			}
			return err
		}
	}

	if rule.redirect != nil {
		redirectTarget := rule.redirect(transition, data)

		if redirectTarget == transition.From {
			return ErrSelfRedirect
		}

		if sm.observer != nil && sm.observer.OnRedirected != nil {
			sm.observer.OnRedirected(transition, redirectTarget, data)
		}

		rule.target = redirectTarget
		rule.targetNode = sm.getOrCreateNode(redirectTarget)
		transition.To = redirectTarget
	}

	sm.innerState = rule.target
	sm.currentState.Store(rule.targetNode.boxedState)
	sm.active = rule.targetNode

	for _, effect := range currentNode.exitEffect {
		effect(transition, data)
	}

	if rule.effect != nil {
		rule.effect(transition, data)
	}

	for _, effect := range rule.targetNode.entryEffect {
		effect(transition, data)
	}

	if sm.observer != nil && sm.observer.OnTransitioned != nil {
		sm.observer.OnTransitioned(transition, data)
	}

	return nil
}
