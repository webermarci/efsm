package efsm_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/webermarci/efsm"
)

type State string
type Event string

const (
	StateIdle    State = "IDLE"
	StateRunning State = "RUNNING"
	StateError   State = "ERROR"

	EventStart Event = "START"
	EventFail  Event = "FAIL"
	EventReset Event = "RESET"
)

type DataContext struct {
	Retries int
}

func TestStateMachine_BasicRouting(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning)

	if state := sm.CurrentState(); state != StateIdle {
		t.Fatalf("expected initial state %v, got %v", StateIdle, state)
	}

	err := sm.Fire(context.Background(), EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if state := sm.CurrentState(); state != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, state)
	}
}

func TestStateMachine_InvalidEvent(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning)

	err := sm.Fire(context.Background(), EventFail, nil)
	if err == nil {
		t.Fatal("expected error on invalid event, got nil")
	}

	if !errors.Is(err, efsm.ErrInvalidEvent) {
		t.Fatalf("expected ErrInvalidEvent, got %v", err)
	}

	if sm.CurrentState() != StateIdle {
		t.Fatalf("expected state to remain %v, got %v", StateIdle, sm.CurrentState())
	}
}

func TestStateMachine_NoTransitionsDefined(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	err := sm.Fire(context.Background(), EventStart, nil)
	if err == nil {
		t.Fatal("expected error for unconfigured state, got nil")
	}

	if !errors.Is(err, efsm.ErrNoTransitions) {
		t.Fatalf("expected ErrNoTransitions, got %v", err)
	}
}

func TestStateMachine_Guard(t *testing.T) {
	t.Parallel()

	errGuardFailed := errors.New("too many retries")

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning,
			efsm.WithGuard(func(ctx context.Context, transition efsm.Transition[State, Event], data *DataContext) error {
				if data.Retries >= 3 {
					return errGuardFailed
				}
				return nil
			}),
		)

	err := sm.Fire(context.Background(), EventStart, &DataContext{Retries: 5})
	if err == nil {
		t.Fatal("expected guard to fail the transition")
	}

	if !errors.Is(err, errGuardFailed) {
		t.Fatalf("expected specific guard error, got %v", err)
	}

	if sm.CurrentState() != StateIdle {
		t.Fatalf("expected state to remain %v after failed guard, got %v", StateIdle, sm.CurrentState())
	}

	err = sm.Fire(context.Background(), EventStart, &DataContext{Retries: 1})
	if err != nil {
		t.Fatalf("expected transition to succeed, got %v", err)
	}

	if sm.CurrentState() != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, sm.CurrentState())
	}
}

func TestStateMachine_Effect(t *testing.T) {
	t.Parallel()

	effectCalled := false

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning,
			efsm.WithEffect(func(ctx context.Context, transition efsm.Transition[State, Event], data *DataContext) {
				effectCalled = true
			}),
		)

	err := sm.Fire(context.Background(), EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if !effectCalled {
		t.Fatal("expected effect to be called on successful transition")
	}
}

func TestStateMachine_AvailableStates(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning).
		Permit(StateIdle, EventFail, StateError)

	states := sm.AvailableStates()
	if len(states) != 1 {
		t.Fatalf("expected 1 available states, got %d", len(states))
	}

	if states[0] != StateIdle {
		t.Fatalf("expected available state to be %v, got %v", StateIdle, states[0])
	}
}

func TestStateMachine_AvailableEvents_Empty(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	events := sm.AvailableEvents()
	if len(events) != 0 {
		t.Fatalf("expected 0 available events, got %d", len(events))
	}
}

func TestStateMachine_AvailableEvents(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning).
		Permit(StateIdle, EventFail, StateError)

	events := sm.AvailableEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 available events, got %d", len(events))
	}

	hasStart := false
	hasFail := false

	for _, e := range events {
		if e == EventStart {
			hasStart = true
		}
		if e == EventFail {
			hasFail = true
		}
	}

	if !hasStart || !hasFail {
		t.Fatalf("missing expected events in AvailableEvents result")
	}
}

func TestStateMachine_AvailableEventsForStates(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle).
		Permit(StateIdle, EventStart, StateRunning).
		Permit(StateIdle, EventFail, StateError).
		Permit(StateRunning, EventReset, StateIdle)

	eventsForStates := sm.AvailableEventsForStates()

	if len(eventsForStates) != 2 {
		t.Fatalf("expected 2 states in events map, got %d", len(eventsForStates))
	}

	idleEvents, ok := eventsForStates[StateIdle]
	if !ok || len(idleEvents) != 2 {
		t.Fatalf("expected 2 events for state %v, got %d", StateIdle, len(idleEvents))
	}

	runningEvents, ok := eventsForStates[StateRunning]
	if !ok || len(runningEvents) != 1 {
		t.Fatalf("expected 1 event for state %v, got %d", StateRunning, len(runningEvents))
	}

	if runningEvents[0] != EventReset {
		t.Fatalf("expected event for state %v to be %v, got %v", StateRunning, EventReset, runningEvents[0])
	}
}

func TestStateMachine_AvailableEventsForStates_Empty(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	eventsForStates := sm.AvailableEventsForStates()
	if len(eventsForStates) != 0 {
		t.Fatalf("expected 0 states in events map, got %d", len(eventsForStates))
	}
}

func TestStateMachine_Concurrency(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](0).
		Permit(0, 1, 1).
		Permit(1, 0, 0)

	var wg sync.WaitGroup
	workers := 100
	iterations := 100

	for range workers {
		wg.Go(func() {
			for range iterations {
				currentState := sm.CurrentState()

				targetEvent := 1
				if currentState == 1 {
					targetEvent = 0
				}

				_ = sm.Fire(context.Background(), targetEvent, nil)
			}
		})
	}

	wg.Wait()
}

func BenchmarkStateMachine_Fire(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0).
		Permit(0, 1, 1).
		Permit(1, 0, 0)

	ctx := context.Background()

	for i := 0; b.Loop(); i++ {
		event := i % 2
		_ = sm.Fire(ctx, event, nil)
	}
}

func BenchmarkStateMachine_State_Parallel(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = sm.CurrentState()
		}
	})
}

func BenchmarkStateMachine_Fire_Parallel(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0).
		Permit(0, 1, 1).
		Permit(1, 0, 0)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			st := sm.CurrentState()
			target := 1
			if st == 1 {
				target = 0
			}
			_ = sm.Fire(ctx, target, nil)
		}
	})
}
