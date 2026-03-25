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

	builder := efsm.NewBuilder[State, Event, *DataContext](StateIdle)

	builder.Configure(StateIdle).
		Permit(EventStart, StateRunning)

	sm := builder.Build()

	if state := sm.State(); state != StateIdle {
		t.Fatalf("expected initial state %v, got %v", StateIdle, state)
	}

	err := sm.Fire(context.Background(), EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if state := sm.State(); state != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, state)
	}
}

func TestStateMachine_InvalidEvent(t *testing.T) {
	t.Parallel()

	builder := efsm.NewBuilder[State, Event, any](StateIdle)
	builder.Configure(StateIdle).Permit(EventStart, StateRunning)
	sm := builder.Build()

	err := sm.Fire(context.Background(), EventFail, nil)
	if err == nil {
		t.Fatal("expected error on invalid event, got nil")
	}

	if !errors.Is(err, efsm.ErrInvalidEvent) {
		t.Fatalf("expected ErrInvalidEvent, got %v", err)
	}

	if sm.State() != StateIdle {
		t.Fatalf("expected state to remain %v, got %v", StateIdle, sm.State())
	}
}

func TestStateMachine_NoTransitionsDefined(t *testing.T) {
	t.Parallel()
	builder := efsm.NewBuilder[State, Event, any](StateIdle)
	sm := builder.Build()

	err := sm.Fire(context.Background(), EventStart, nil)
	if err == nil {
		t.Fatal("expected error for unconfigured state, got nil")
	}

	if !errors.Is(err, efsm.ErrNoTransitions) {
		t.Fatalf("expected ErrNoTransitions, got %v", err)
	}
}

func TestStateMachine_GuardAction(t *testing.T) {
	t.Parallel()

	builder := efsm.NewBuilder[State, Event, *DataContext](StateIdle)

	errActionFailed := errors.New("too many retries")

	builder.Configure(StateIdle).
		PermitWithAction(
			EventStart,
			StateRunning,
			func(ctx context.Context, transition efsm.Transition[State, Event], data *DataContext) error {
				if data.Retries >= 3 {
					return errActionFailed
				}
				return nil
			},
		)

	sm := builder.Build()

	err := sm.Fire(context.Background(), EventStart, &DataContext{Retries: 5})
	if err == nil {
		t.Fatal("expected guard action to fail the transition")
	}

	if !errors.Is(err, errActionFailed) {
		t.Fatalf("expected specific guard error, got %v", err)
	}

	if sm.State() != StateIdle {
		t.Fatalf("expected state to remain %v after failed guard, got %v", StateIdle, sm.State())
	}

	err = sm.Fire(context.Background(), EventStart, &DataContext{Retries: 1})
	if err != nil {
		t.Fatalf("expected transition to succeed, got %v", err)
	}

	if sm.State() != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, sm.State())
	}
}

func TestStateMachine_AvailableEvents(t *testing.T) {
	t.Parallel()

	builder := efsm.NewBuilder[State, Event, any](StateIdle)
	builder.Configure(StateIdle).
		Permit(EventStart, StateRunning).
		Permit(EventFail, StateError)

	sm := builder.Build()

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

func TestStateMachine_Concurrency(t *testing.T) {
	t.Parallel()

	builder := efsm.NewBuilder[int, int, any](0)

	builder.Configure(0).Permit(1, 1)
	builder.Configure(1).Permit(0, 0)
	sm := builder.Build()

	var wg sync.WaitGroup
	workers := 100
	iterations := 100

	for range workers {
		wg.Go(func() {
			for range iterations {
				currentState := sm.State()

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
	builder := efsm.NewBuilder[int, int, any](0)
	builder.Configure(0).Permit(1, 1)
	builder.Configure(1).Permit(0, 0)
	sm := builder.Build()

	ctx := context.Background()

	for i := 0; b.Loop(); i++ {
		event := i % 2
		_ = sm.Fire(ctx, event, nil)
	}
}

func BenchmarkStateMachine_State_Parallel(b *testing.B) {
	builder := efsm.NewBuilder[int, int, any](0)
	sm := builder.Build()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = sm.State()
		}
	})
}

func BenchmarkStateMachine_Fire_Parallel(b *testing.B) {
	builder := efsm.NewBuilder[int, int, any](0)
	builder.Configure(0).Permit(1, 1)
	builder.Configure(1).Permit(0, 0)
	sm := builder.Build()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			st := sm.State()
			target := 1
			if st == 1 {
				target = 0
			}
			_ = sm.Fire(ctx, target, nil)
		}
	})
}
