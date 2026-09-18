//go:build bend

package main

import (
	"os"
	"testing"
)

func TestBendNativeDeliveryAgreement(t *testing.T) {
	table, err := readDeliveryTable(os.Getenv("TETHER_BEND_DELIVERY"))
	if err != nil {
		t.Fatal(err)
	}
	for confirmed := 0; confirmed <= 5; confirmed++ {
		for flags := 0; flags < 8; flags++ {
			state := deliveryState{confirmed: confirmed, failed: flags&2 != 0}
			expected := state
			if flags&1 != 0 && !state.failed {
				if flags&4 != 0 {
					expected.confirmed++
				} else {
					expected.failed = true
				}
			}
			actual, err := bendDeliveryStep(table, flags&1 != 0, state, flags&4 != 0)
			if err != nil || actual != expected {
				t.Fatalf("confirmed=%d flags=%d got=%+v want=%+v error=%v", confirmed, flags, actual, expected, err)
			}
		}
	}
}
