package efsm_test

import (
	"errors"
	"testing"

	"github.com/webermarci/efsm"
)

func FuzzStateMachine(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{1, 255, 7, 3, 2, 0})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, input []byte) {
		guardErr := errors.New("fuzz guard rejected event")
		sm := efsm.NewStateMachine[int, int, int](0,
			efsm.WithState(0,
				efsm.WithPermit(0, 1),
				efsm.WithPermit(1, 0, efsm.WithGuard(func(_ efsm.Transition[int, int], data int) error {
					if data%2 == 0 {
						return guardErr
					}
					return nil
				})),
				efsm.WithPermitRedirect(2, func(_ efsm.Transition[int, int], data int) int {
					return data % 3
				}),
				efsm.WithPermit(3, 2),
			),
			efsm.WithState(1,
				efsm.WithPermit(0, 2),
				efsm.WithPermit(1, 1),
				efsm.WithPermitRedirect(2, func(_ efsm.Transition[int, int], data int) int {
					return data % 3
				}),
				efsm.WithPermit(3, 0),
			),
			efsm.WithState(2,
				efsm.WithPermit(0, 0),
				efsm.WithPermit(1, 2),
				efsm.WithPermitRedirect(2, func(_ efsm.Transition[int, int], data int) int {
					return data % 3
				}),
				efsm.WithPermit(3, 1),
			),
		)

		for index, value := range input {
			before := sm.CurrentState()
			transition, err := sm.Fire(int(value%5), index)
			if transition.From != before {
				t.Fatalf("transition.From = %v, want %v", transition.From, before)
			}
			if err != nil {
				if sm.CurrentState() != before {
					t.Fatalf("rejected event changed state from %v to %v", before, sm.CurrentState())
				}
				continue
			}
			if transition.To != sm.CurrentState() {
				t.Fatalf("transition.To = %v, current state = %v", transition.To, sm.CurrentState())
			}
		}
	})
}
