//go:build bend

package main

import (
	"os"
	"testing"
)

func TestBendNativeDispatchAgreement(t *testing.T) {
	table, err := readBendTable(os.Getenv("TETHER_BEND_DISPATCH"), dispatchProtocol)
	if err != nil {
		t.Fatal(err)
	}
	for _, quiet := range []bool{false, true} {
		for fired := 0; fired <= 9; fired++ {
			for candidates := 0; candidates <= 9; candidates++ {
				want := 0
				if !quiet {
					want = min(max(0, 5-fired), candidates)
				}
				if got := bendBatchCount(table, quiet, fired, candidates); got != want {
					t.Fatalf("quiet=%t fired=%d candidates=%d got=%d want=%d", quiet, fired, candidates, got, want)
				}
			}
		}
	}
}
