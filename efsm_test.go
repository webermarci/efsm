package efsm_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

import "github.com/webermarci/efsm"

type State string
type Event string

const (
	StateIdle    State = "IDLE"
	StateRunning State = "RUNNING"
	StateError   State = "ERROR"
	StateOther   State = "OTHER"

	EventStart    Event = "START"
	EventReset    Event = "RESET"
	EventFail     Event = "FAIL"
	EventRedirect Event = "REDIRECT"
)

type DataContext struct {
	Retries int
}

func newBasicMachine() *efsm.StateMachine[State, Event, *DataContext] {
	return efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermit(EventStart, StateRunning),
		),
		efsm.WithState(StateRunning,
			efsm.WithPermit(EventReset, StateIdle),
		),
	)
}

func TestStateMachine_BasicRouting(t *testing.T) {
	t.Parallel()

	sm := newBasicMachine()
	if state := sm.CurrentState(); state != StateIdle {
		t.Fatalf("initial state = %v, want %v", state, StateIdle)
	}

	transition, err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if want := (efsm.Transition[State, Event]{From: StateIdle, To: StateRunning, Event: EventStart}); transition != want {
		t.Fatalf("transition = %+v, want %+v", transition, want)
	}
	if state := sm.CurrentState(); state != StateRunning {
		t.Fatalf("current state = %v, want %v", state, StateRunning)
	}
}

func TestStateMachine_ImmutableConfiguration(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](
		0,
		efsm.WithState(0,
			efsm.WithPermit(1, 1),
			efsm.WithPermit(1, 2),
		),
	)

	if _, err := sm.Fire(1, nil); err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if state := sm.CurrentState(); state != 2 {
		t.Fatalf("last permit did not replace the first: state = %v, want 2", state)
	}
	if got, want := sm.AvailableStates(), []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

func TestStateMachine_ReusableStateOptions(t *testing.T) {
	t.Parallel()

	var entries []State
	telemetryEntry := efsm.OnEntry(func(t efsm.Transition[State, Event], _ *DataContext) {
		entries = append(entries, t.To)
	})
	telemetryExit := efsm.OnExit(func(_ efsm.Transition[State, Event], _ *DataContext) {})

	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle, telemetryEntry, telemetryExit,
			efsm.WithPermit(EventStart, StateRunning)),
		efsm.WithState(StateRunning, telemetryEntry, telemetryExit,
			efsm.WithPermit(EventReset, StateIdle)),
	)

	if _, err := sm.Fire(EventStart, nil); err != nil {
		t.Fatalf("start Fire() error = %v", err)
	}
	if _, err := sm.Fire(EventReset, nil); err != nil {
		t.Fatalf("reset Fire() error = %v", err)
	}
	if want := []State{StateRunning, StateIdle}; !reflect.DeepEqual(entries, want) {
		t.Fatalf("entry states = %v, want %v", entries, want)
	}
}

func TestStateMachine_GuardAndErrorContext(t *testing.T) {
	t.Parallel()

	guardErr := errors.New("blocked")
	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermit(EventStart, StateRunning, efsm.WithGuard(func(efsm.Transition[State, Event], *DataContext) error {
				return guardErr
			})),
		),
	)

	transition, err := sm.Fire(EventStart, nil)
	if !errors.Is(err, guardErr) {
		t.Fatalf("Fire() error = %v, want wrapped guard error", err)
	}
	if !strings.Contains(err.Error(), "event START") || !strings.Contains(err.Error(), "state IDLE") {
		t.Fatalf("guard error lacks context: %v", err)
	}
	if transition.To != StateRunning || sm.CurrentState() != StateIdle {
		t.Fatalf("guard rejection changed transition/state: transition=%+v state=%v", transition, sm.CurrentState())
	}

	_, err = sm.Fire(EventReset, nil)
	if !errors.Is(err, efsm.ErrInvalidEvent) || !strings.Contains(err.Error(), "event RESET") {
		t.Fatalf("invalid event error = %v", err)
	}
}

func TestStateMachine_CheckValidatesWithoutMutating(t *testing.T) {
	t.Parallel()

	var calls []string
	guardErr := errors.New("blocked")
	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.OnExit(func(efsm.Transition[State, Event], *DataContext) {
				calls = append(calls, "exit")
			}),
			efsm.WithPermit(EventStart, StateRunning,
				efsm.WithGuard(func(_ efsm.Transition[State, Event], data *DataContext) error {
					calls = append(calls, "guard")
					if data.Retries < 0 {
						return guardErr
					}
					return nil
				}),
				efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
					calls = append(calls, "transition")
				}),
			),
		),
		efsm.WithState(StateRunning,
			efsm.OnEntry(func(efsm.Transition[State, Event], *DataContext) {
				calls = append(calls, "entry")
			}),
		),
	)

	transition, err := sm.Check(EventStart, &DataContext{Retries: 1})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if want := (efsm.Transition[State, Event]{From: StateIdle, To: StateRunning, Event: EventStart}); transition != want {
		t.Fatalf("transition = %+v, want %+v", transition, want)
	}
	if state := sm.CurrentState(); state != StateIdle {
		t.Fatalf("current state after Check() = %v, want %v", state, StateIdle)
	}
	if want := []string{"guard"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("callbacks after Check() = %v, want %v", calls, want)
	}

	transition, err = sm.Check(EventStart, &DataContext{Retries: -1})
	if !errors.Is(err, guardErr) {
		t.Fatalf("rejected Check() error = %v, want wrapped guard error", err)
	}
	if transition.To != StateRunning || sm.CurrentState() != StateIdle {
		t.Fatalf("rejected Check() changed transition/state: transition=%+v state=%v", transition, sm.CurrentState())
	}
	if want := []string{"guard", "guard"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("callbacks after rejected Check() = %v, want %v", calls, want)
	}
}

func TestStateMachine_CheckResolvesRedirectAndReportsErrors(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermitRedirect(EventRedirect, func(_ efsm.Transition[State, Event], data *DataContext) State {
				if data.Retries > 0 {
					return StateRunning
				}
				return StateError
			}),
		),
		efsm.WithState[State, Event](StateRunning),
	)

	transition, err := sm.Check(EventRedirect, &DataContext{Retries: 1})
	if err != nil || transition.To != StateRunning {
		t.Fatalf("redirect Check() = (%+v, %v), want target RUNNING", transition, err)
	}
	if sm.CurrentState() != StateIdle {
		t.Fatalf("current state after redirect Check() = %v, want %v", sm.CurrentState(), StateIdle)
	}

	unknown := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermitRedirect(EventRedirect, func(_ efsm.Transition[State, Event], _ *DataContext) State {
				return StateOther
			}),
		),
	)
	transition, err = unknown.Check(EventRedirect, nil)
	if !errors.Is(err, efsm.ErrUnknownState) || transition.To != StateOther {
		t.Fatalf("unknown redirect Check() = (%+v, %v), want unknown-state error", transition, err)
	}
	if unknown.CurrentState() != StateIdle {
		t.Fatalf("current state after unknown redirect Check() = %v, want %v", unknown.CurrentState(), StateIdle)
	}
}

func TestStateMachine_EffectOrderAndCommittedState(t *testing.T) {
	t.Parallel()

	var calls []string
	var sm *efsm.StateMachine[State, Event, *DataContext]
	record := func(name string) efsm.Effect[State, Event, *DataContext] {
		return func(efsm.Transition[State, Event], *DataContext) {
			calls = append(calls, name+":"+string(sm.CurrentState()))
			_ = sm.AvailableStates()
			_ = sm.AvailableEvents()
		}
	}

	sm = efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.OnExit(func(efsm.Transition[State, Event], *DataContext) {
				calls = append(calls, "exit:"+string(sm.CurrentState()))
			}),
			efsm.WithPermit(EventStart, StateRunning, efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
				calls = append(calls, "transition:"+string(sm.CurrentState()))
			})),
		),
		efsm.WithState(StateRunning,
			efsm.OnEntry(record("entry")),
		),
	)

	if _, err := sm.Fire(EventStart, nil); err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if want := []string{"exit:RUNNING", "transition:RUNNING", "entry:RUNNING"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("effect order = %v, want %v", calls, want)
	}
}

func TestStateMachine_InvalidEvent(t *testing.T) {
	t.Parallel()

	noTransitions := efsm.NewStateMachine[int, int, any](0)
	_, err := noTransitions.Fire(1, nil)
	if !errors.Is(err, efsm.ErrInvalidEvent) || !strings.Contains(err.Error(), "event 1") || !strings.Contains(err.Error(), "state 0") {
		t.Fatalf("no-transition error = %v", err)
	}

	sm := efsm.NewStateMachine[int, int, any](0, efsm.WithState(0, efsm.WithPermit(1, 1)))
	_, err = sm.Fire(2, nil)
	if !errors.Is(err, efsm.ErrInvalidEvent) || !strings.Contains(err.Error(), "event 2") {
		t.Fatalf("invalid-event error = %v", err)
	}
}

func TestStateMachine_CallbackPanicsReleaseLock(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermit(EventStart, StateRunning, efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
				panic("effect failed")
			})),
		),
		efsm.WithState(StateRunning, efsm.WithPermit(EventReset, StateIdle)),
	)

	func() {
		defer func() {
			if recover() != "effect failed" {
				t.Fatal("expected effect panic")
			}
		}()
		_, _ = sm.Fire(EventStart, nil)
	}()

	if sm.CurrentState() != StateRunning {
		t.Fatalf("state after panic = %v, want %v", sm.CurrentState(), StateRunning)
	}
	if _, err := sm.Fire(EventReset, nil); err != nil {
		t.Fatalf("Fire() after panic = %v", err)
	}
}

func TestStateMachine_NilOptionsPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func()
	}{
		{"state option", func() {
			efsm.NewStateMachine[int, int, any](0, efsm.WithState[int, int](0, nil))
		}},
		{"transition option", func() {
			efsm.NewStateMachine[int, int, any](0, efsm.WithState(0, efsm.WithPermit(1, 1, nil)))
		}},
		{"entry effect", func() {
			efsm.OnEntry[int, int, any](nil)
		}},
		{"exit effect", func() {
			efsm.OnExit[int, int, any](nil)
		}},
		{"redirect", func() {
			efsm.WithPermitRedirect[int, int, any](1, nil)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			test.call()
		})
	}
}
