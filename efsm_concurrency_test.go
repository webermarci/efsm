package efsm_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/webermarci/efsm"
)

func TestStateMachine_Concurrency(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](0,
		efsm.WithState(0, efsm.WithPermit(1, 1)),
		efsm.WithState(1, efsm.WithPermit(1, 0)),
	)

	var wait sync.WaitGroup
	var errorsSeen atomic.Uint64
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				if _, err := sm.Fire(1, nil); err != nil {
					errorsSeen.Add(1)
				}
			}
		}()
	}
	wait.Wait()

	if count := errorsSeen.Load(); count != 0 {
		t.Fatalf("concurrent Fire() errors = %d, want 0", count)
	}
}

func TestStateMachine_CurrentStateAndInspectionConcurrentWithFire(t *testing.T) {
	t.Parallel()

	sm := efsm.NewStateMachine[int, int, any](0,
		efsm.WithState(0, efsm.WithPermit(1, 1)),
		efsm.WithState(1, efsm.WithPermit(1, 0)),
	)

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for range 10000 {
			_, _ = sm.Fire(1, nil)
		}
	}()
	go func() {
		defer wait.Done()
		for range 10000 {
			_ = sm.CurrentState()
			_ = sm.AvailableStates()
			_ = sm.AvailableEvents()
			_ = sm.AvailableEventsForStates()
		}
	}()
	wait.Wait()
}
