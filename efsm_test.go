package efsm_test

import (
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

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	if state := sm.CurrentState(); state != StateIdle {
		t.Fatalf("expected initial state %v, got %v", StateIdle, state)
	}

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if state := sm.CurrentState(); state != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, state)
	}
}

func TestStateMachine_InvalidEvent(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	err := sm.Fire(EventFail, nil)
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

	err := sm.Fire(EventStart, nil)
	if err == nil {
		t.Fatal("expected error for unconfigured state, got nil")
	}

	if !errors.Is(err, efsm.ErrNoTransitions) {
		t.Fatalf("expected ErrNoTransitions, got %v", err)
	}
}

func TestStateMachine_OnEntry(t *testing.T) {
	t.Parallel()

	entryCalled := false

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	sm.Configure(StateRunning, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(transition efsm.Transition[State, Event], data *DataContext) {
			entryCalled = true
		})
	})

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if !entryCalled {
		t.Fatal("expected OnEntry effect to be called on successful transition")
	}
}

func TestStateMachine_OnExit(t *testing.T) {
	t.Parallel()

	exitCalled := false

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnExit(func(transition efsm.Transition[State, Event], data *DataContext) {
			exitCalled = true
		})
		c.Permit(EventStart, StateRunning)
	})

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if !exitCalled {
		t.Fatal("expected OnExit effect to be called on successful transition")
	}
}

func TestStateMachine_Guard(t *testing.T) {
	t.Parallel()

	errGuardFailed := errors.New("too many retries")

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning,
			efsm.WithGuard(func(transition efsm.Transition[State, Event], data *DataContext) error {
				if data.Retries >= 3 {
					return errGuardFailed
				}
				return nil
			}),
		)
	})

	err := sm.Fire(EventStart, &DataContext{Retries: 5})
	if err == nil {
		t.Fatal("expected guard to fail the transition")
	}

	if !errors.Is(err, errGuardFailed) {
		t.Fatalf("expected specific guard error, got %v", err)
	}

	if sm.CurrentState() != StateIdle {
		t.Fatalf("expected state to remain %v after failed guard, got %v", StateIdle, sm.CurrentState())
	}

	err = sm.Fire(EventStart, &DataContext{Retries: 1})
	if err != nil {
		t.Fatalf("expected transition to succeed, got %v", err)
	}

	if sm.CurrentState() != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, sm.CurrentState())
	}
}

func TestStateMachine_PermitRedirect(t *testing.T) {
	t.Parallel()

	sm1 := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm1.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.PermitRedirect(EventStart, func(t efsm.Transition[State, Event], d *DataContext) State {
			if d.Retries > 3 {
				return StateError
			}
			return StateRunning
		})
	})

	err := sm1.Fire(EventStart, &DataContext{Retries: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state := sm1.CurrentState(); state != StateRunning {
		t.Fatalf("expected state %v, got %v", StateRunning, state)
	}

	sm2 := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm2.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.PermitRedirect(EventStart, func(t efsm.Transition[State, Event], d *DataContext) State {
			if d.Retries > 3 {
				return StateError
			}
			return StateRunning
		})
	})

	err2 := sm2.Fire(EventStart, &DataContext{Retries: 5})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}

	if state := sm2.CurrentState(); state != StateError {
		t.Fatalf("expected state %v, got %v", StateError, state)
	}
}

func TestStateMachine_RedirectLoopPrevention(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.PermitRedirect(EventStart, func(t efsm.Transition[State, Event], d *DataContext) State {
			return StateIdle
		})
	})

	err := sm.Fire(EventStart, nil)
	if err == nil {
		t.Fatal("expected error on self-redirect, got nil")
	}

	if !errors.Is(err, efsm.ErrSelfRedirect) {
		t.Fatalf("expected ErrSelfRedirect, got %v", err)
	}

	if state := sm.CurrentState(); state != StateIdle {
		t.Fatalf("expected state to remain %v, got %v", StateIdle, state)
	}
}

func TestStateMachine_RedirectEffectsContext(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	var targetInEffect State
	var entryCalledForError bool
	var entryCalledForRunning bool

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.PermitRedirect(EventStart, func(t efsm.Transition[State, Event], d *DataContext) State {
			return StateError
		}, efsm.OnTransition(func(t efsm.Transition[State, Event], d *DataContext) {
			targetInEffect = t.To
		}))
	})

	sm.Configure(StateRunning, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(t efsm.Transition[State, Event], d *DataContext) {
			entryCalledForRunning = true
		})
	})

	sm.Configure(StateError, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(t efsm.Transition[State, Event], d *DataContext) {
			entryCalledForError = true
		})
	})

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if targetInEffect != StateError {
		t.Errorf("expected transition effect to receive To=%v, got To=%v", StateError, targetInEffect)
	}

	if entryCalledForRunning {
		t.Error("expected StateRunning entry effect NOT to be called")
	}

	if !entryCalledForError {
		t.Error("expected StateError entry effect to be called")
	}
}

func TestStateMachine_RedirectOverwriteOptions(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	redirect1Called := false
	redirect2Called := false

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning,
			efsm.WithRedirect(func(t efsm.Transition[State, Event], d *DataContext) State {
				redirect1Called = true
				return t.To
			}),
			efsm.WithRedirect(func(t efsm.Transition[State, Event], d *DataContext) State {
				redirect2Called = true
				return StateError
			}),
		)
	})

	_ = sm.Fire(EventStart, nil)

	if redirect1Called {
		t.Error("expected redirect1 to be overwritten, but it was called")
	}

	if !redirect2Called {
		t.Error("expected redirect2 to be called")
	}

	if sm.CurrentState() != StateError {
		t.Errorf("expected final state to be %v (from redirect2), got %v", StateError, sm.CurrentState())
	}
}

func TestStateMachine_Effect(t *testing.T) {
	t.Parallel()

	effectCalled := false

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning,
			efsm.OnTransition(func(transition efsm.Transition[State, Event], data *DataContext) {
				effectCalled = true
			}),
		)
	})

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}

	if !effectCalled {
		t.Fatal("expected effect to be called on successful transition")
	}
}

func TestStateMachine_EffectExecutionOrder(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	var executionOrder []string
	var mu sync.Mutex

	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, step)
	}

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnExit(func(efsm.Transition[State, Event], *DataContext) {
			record("exit_idle")
		})
		c.Permit(EventStart, StateRunning, efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
			record("transition_start")
		}))
	})

	sm.Configure(StateRunning, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(efsm.Transition[State, Event], *DataContext) {
			record("entry_running")
		})
	})

	err := sm.Fire(EventStart, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 effects to fire, got %d", len(executionOrder))
	}

	if executionOrder[0] != "exit_idle" {
		t.Errorf("expected first effect to be exit_idle, got %s", executionOrder[0])
	}

	if executionOrder[1] != "transition_start" {
		t.Errorf("expected second effect to be transition_start, got %s", executionOrder[1])
	}

	if executionOrder[2] != "entry_running" {
		t.Errorf("expected third effect to be entry_running, got %s", executionOrder[2])
	}
}

func TestStateMachine_MultipleMixinsEffects(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	var executionOrder []string
	var mu sync.Mutex

	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, step)
	}

	mixin1 := func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(efsm.Transition[State, Event], *DataContext) {
			record("mixin1_entry")
		})
	}

	mixin2 := func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(efsm.Transition[State, Event], *DataContext) {
			record("mixin2_entry")
		})
	}

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	sm.Configure(StateRunning, mixin1, mixin2, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnEntry(func(efsm.Transition[State, Event], *DataContext) {
			record("state_specific_entry")
		})
	})

	_ = sm.Fire(EventStart, nil)

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 entry effects to fire via mixin composition, got %d", len(executionOrder))
	}

	if executionOrder[0] != "mixin1_entry" || executionOrder[1] != "mixin2_entry" || executionOrder[2] != "state_specific_entry" {
		t.Errorf("unexpected mixin execution order: %v", executionOrder)
	}
}

func TestStateMachine_SelfTransition(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateRunning)

	var executionOrder []string
	var mu sync.Mutex

	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, step)
	}

	sm.Configure(StateRunning, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.OnExit(func(efsm.Transition[State, Event], *DataContext) { record("exit") })
		c.OnEntry(func(efsm.Transition[State, Event], *DataContext) { record("entry") })
		c.Permit(EventReset, StateRunning, efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
			record("transition")
		}))
	})

	err := sm.Fire(EventReset, nil)
	if err != nil {
		t.Fatalf("unexpected error on self-transition: %v", err)
	}

	if sm.CurrentState() != StateRunning {
		t.Fatalf("expected state to remain %v, got %v", StateRunning, sm.CurrentState())
	}

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 effects, got %d", len(executionOrder))
	}

	if executionOrder[0] != "exit" || executionOrder[1] != "transition" || executionOrder[2] != "entry" {
		t.Errorf("unexpected execution order for self-transition: %v", executionOrder)
	}
}

func TestStateMachine_OverwriteOptions(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	guard1Called, guard2Called := false, false
	effect1Called, effect2Called := false, false

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning,
			efsm.WithGuard(func(efsm.Transition[State, Event], *DataContext) error {
				guard1Called = true
				return nil
			}),
			efsm.WithGuard(func(efsm.Transition[State, Event], *DataContext) error {
				guard2Called = true
				return nil
			}),
			efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
				effect1Called = true
			}),
			efsm.OnTransition(func(efsm.Transition[State, Event], *DataContext) {
				effect2Called = true
			}),
		)
	})

	_ = sm.Fire(EventStart, nil)

	if guard1Called {
		t.Error("expected guard1 to be overwritten, but it was called")
	}

	if !guard2Called {
		t.Error("expected guard2 to be called")
	}

	if effect1Called {
		t.Error("expected effect1 to be overwritten, but it was called")
	}

	if !effect2Called {
		t.Error("expected effect2 to be called")
	}
}

func TestStateMachine_DataPropagation(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)
	testData := &DataContext{Retries: 42}

	var dataObserved *DataContext

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning,
			efsm.OnTransition(func(_ efsm.Transition[State, Event], d *DataContext) {
				dataObserved = d
			}),
		)
	})

	_ = sm.Fire(EventStart, testData)

	if dataObserved == nil || dataObserved.Retries != 42 {
		t.Errorf("expected data context with Retries 42, got %v", dataObserved)
	}
}

func TestStateMachine_AvailableStates(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
		c.Permit(EventFail, StateError)
	})

	states := sm.AvailableStates()
	if len(states) != 3 {
		t.Fatalf("expected 3 available states, got %d", len(states))
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

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
		c.Permit(EventFail, StateError)
	})

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

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
		c.Permit(EventFail, StateError)
	})

	sm.Configure(StateRunning, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventReset, StateIdle)
	})

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

func TestStateMachine_CanFire(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	if !sm.CanFire(EventStart) {
		t.Errorf("expected CanFire to return true for valid event")
	}

	if sm.CanFire(EventFail) {
		t.Errorf("expected CanFire to return false for invalid event")
	}
}

func TestStateMachine_MustFire(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[State, Event, *DataContext](StateIdle)

	sm.Configure(StateIdle, func(c *efsm.StateConfigurator[State, Event, *DataContext]) {
		c.Permit(EventStart, StateRunning)
	})

	// This should panic
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected MustFire to panic on invalid transition")
		}
	}()

	sm.MustFire(EventFail, nil)
}

func TestStateMachine_Concurrency(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(1, 1) })
	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(0, 0) })

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

				_ = sm.Fire(targetEvent, nil)
			}
		})
	}

	wg.Wait()
}

func BenchmarkStateMachine_Fire(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(1, 1) })
	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(0, 0) })

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		event := i % 2
		_ = sm.Fire(event, nil)
	}
}

func BenchmarkStateMachine_FireEffects(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) {
		c.OnExit(func(efsm.Transition[int, int], any) {})
		c.Permit(1, 1, efsm.WithGuard(func(efsm.Transition[int, int], any) error { return nil }),
			efsm.OnTransition(func(efsm.Transition[int, int], any) {}))
	})

	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) {
		c.OnEntry(func(efsm.Transition[int, int], any) {})
		c.Permit(0, 0)
	})

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		event := i % 2
		_ = sm.Fire(event, nil)
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
	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(1, 1) })
	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) { c.Permit(0, 0) })

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			st := sm.CurrentState()
			target := 1
			if st == 1 {
				target = 0
			}
			_ = sm.Fire(target, nil)
		}
	})
}

func BenchmarkStateMachine_FireRedirect(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) {
		c.PermitRedirect(1, func(efsm.Transition[int, int], any) int {
			return 1
		})
	})
	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) {
		c.PermitRedirect(0, func(efsm.Transition[int, int], any) int {
			return 0
		})
	})

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		event := 1
		if i%2 != 0 {
			event = 0
		}
		_ = sm.Fire(event, nil)
	}
}

func BenchmarkStateMachine_FireRedirect_Parallel(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)

	sm.Configure(0, func(c *efsm.StateConfigurator[int, int, any]) {
		c.PermitRedirect(1, func(efsm.Transition[int, int], any) int {
			return 1
		})
	})
	sm.Configure(1, func(c *efsm.StateConfigurator[int, int, any]) {
		c.PermitRedirect(0, func(efsm.Transition[int, int], any) int {
			return 0
		})
	})

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			st := sm.CurrentState()
			target := 1
			if st == 1 {
				target = 0
			}
			_ = sm.Fire(target, nil)
		}
	})
}
