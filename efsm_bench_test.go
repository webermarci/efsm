package efsm_test

import (
	"testing"

	"github.com/webermarci/efsm"
)

func BenchmarkStateMachine_Fire(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0,
		efsm.WithState(0, efsm.WithPermit(1, 1)),
		efsm.WithState(1, efsm.WithPermit(0, 0)),
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_, _ = sm.Fire(i%2, nil)
	}
}

func BenchmarkStateMachine_FireInvalidEvent(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0, efsm.WithState(0, efsm.WithPermit(1, 1)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = sm.Fire(2, nil)
	}
}

func BenchmarkStateMachine_FireEffects(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0,
		efsm.WithState(0,
			efsm.OnExit(func(efsm.Transition[int, int], any) {}),
			efsm.WithPermit(1, 1,
				efsm.WithGuard(func(efsm.Transition[int, int], any) error { return nil }),
				efsm.OnTransition(func(efsm.Transition[int, int], any) {})),
		),
		efsm.WithState(1,
			efsm.OnEntry(func(efsm.Transition[int, int], any) {}),
			efsm.WithPermit(0, 0),
		),
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_, _ = sm.Fire(i%2, nil)
	}
}

func BenchmarkStateMachine_CurrentStateParallel(b *testing.B) {
	sm := efsm.NewStateMachine[int, int, any](0)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = sm.CurrentState()
		}
	})
}
