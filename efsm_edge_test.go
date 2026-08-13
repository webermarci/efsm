package efsm_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/webermarci/efsm"
)

func TestStateMachine_InterfaceStateAndEventTypes(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[any, any, any](1,
		efsm.WithState[any, any](1, efsm.WithPermit[any, any]("start", "running")),
	)
	if _, err := sm.Fire("start", nil); err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if state := sm.CurrentState(); state != "running" {
		t.Fatalf("state = %v, want running", state)
	}
}

func TestStateMachine_InspectionOrder(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](0,
		efsm.WithState(0,
			efsm.WithPermit(20, 1),
			efsm.WithPermit(10, 2),
			efsm.WithPermit(20, 3),
		),
		efsm.WithState(4, efsm.WithPermit(30, 5)),
	)

	if got, want := sm.AvailableStates(), []int{0, 1, 2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	if got, want := sm.AvailableEvents(), []int{20, 10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if got, want := sm.AvailableEventsForStates()[4], []int{30}; !reflect.DeepEqual(got, want) {
		t.Fatalf("state 4 events = %v, want %v", got, want)
	}
}

func TestStateMachine_RedirectRequiresDeclaredTarget(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](
		StateIdle,
		efsm.WithState(StateIdle,
			efsm.WithPermitRedirect(EventRedirect, func(efsm.Transition[State, Event], *DataContext) State {
				return StateError
			}),
		),
		efsm.WithState[State, Event](StateError),
	)

	transition, err := sm.Fire(EventRedirect, nil)
	if err != nil || transition.To != StateError || sm.CurrentState() != StateError {
		t.Fatalf("redirect = (%+v, %v), state = %v", transition, err, sm.CurrentState())
	}

	unknown := efsm.NewStateMachine[State, Event, *DataContext](StateIdle,
		efsm.WithState(StateIdle, efsm.WithPermitRedirect(EventRedirect, func(efsm.Transition[State, Event], *DataContext) State {
			return StateOther
		})),
	)
	transition, err = unknown.Fire(EventRedirect, nil)
	if !errors.Is(err, efsm.ErrUnknownState) || transition.To != StateOther || unknown.CurrentState() != StateIdle {
		t.Fatalf("unknown redirect = (%+v, %v), state = %v", transition, err, unknown.CurrentState())
	}
	if !strings.Contains(err.Error(), "target OTHER") {
		t.Fatalf("unknown redirect error lacks context: %v", err)
	}
}

func TestStateMachine_SelfRedirectIsAllowed(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle,
		efsm.WithState(StateIdle, efsm.WithPermitRedirect(EventRedirect, func(efsm.Transition[State, Event], *DataContext) State {
			return StateIdle
		})),
	)

	transition, err := sm.Fire(EventRedirect, nil)
	if err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if transition.From != StateIdle || transition.To != StateIdle {
		t.Fatalf("transition = %+v, want idle -> idle", transition)
	}
	if sm.CurrentState() != StateIdle {
		t.Fatalf("state = %v, want idle", sm.CurrentState())
	}
}
