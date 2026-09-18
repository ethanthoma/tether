package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBendDispatchSwitch(t *testing.T) {
	original := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	for _, mode := range []string{"good", "five", "error", "version", "decision", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			store := testStore(t)
			store.Reminders = []*Reminder{{ID: "test", Text: "test", State: ReminderOpen, Due: now.Add(-time.Hour)}}
			cfg := &Config{BendDispatch: shadowHelper(t, "dispatch-"+mode), DiscordChannel: "test"}
			if got := productionBendBatchCount(store, cfg, false, 0, 1); got != 1 {
				t.Fatal("disabled switch changed Go batch")
			}
			marker := filepath.Join(store.dir, "bend-dispatch.enabled")
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			*transport = bendDiscordTransport{}
			if err := RunNudges(store, cfg, now); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "good" {
				want = 0
			}
			if transport.sent != want {
				t.Fatalf("sent=%d want=%d", transport.sent, want)
			}
			nl, err := openNudgeLog(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(nl.records) != want {
				t.Fatal("incorrect confirmations")
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if got := productionBendBatchCount(store, cfg, false, 0, 1); got != 1 {
				t.Fatal("rollback did not restore Go")
			}
		})
	}
}

func TestBendDispatchSafetyLimits(t *testing.T) {
	store := testStore(t)
	cfg := &Config{BendDispatch: shadowHelper(t, "dispatch-five")}
	if err := os.WriteFile(filepath.Join(store.dir, "bend-dispatch.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct {
		quiet                   bool
		fired, candidates, want int
	}{
		{true, 0, 8, 0}, {false, 5, 8, 0}, {false, 4, 8, 1}, {false, 0, 2, 2},
	} {
		if got := productionBendBatchCount(store, cfg, example.quiet, example.fired, example.candidates); got != example.want {
			t.Fatalf("unsafe batch=%d want=%d", got, example.want)
		}
	}
}

func TestBendDispatchShadowDoesNotMutate(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	commitment := &Commitment{ID: "overdue", State: CommitmentOpen, Due: now.Add(-slipGrace - time.Hour)}
	store.Commitments = []*Commitment{commitment}
	output, err := RunBendDispatchShadow(store, shadowHelper(t, "dispatch-good"), now)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status     string `json:"status"`
		Candidates int    `json:"candidates"`
		GoCount    int    `json:"go_batch"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "mismatch" || report.Candidates != 1 || report.GoCount != 1 {
		t.Fatalf("wrong report: %s", output)
	}
	if commitment.State != CommitmentOpen {
		t.Fatal("shadow changed commitment")
	}
	if _, err := os.Stat(filepath.Join(store.dir, "nudges.jsonl")); !os.IsNotExist(err) {
		t.Fatal("shadow wrote log")
	}
}
