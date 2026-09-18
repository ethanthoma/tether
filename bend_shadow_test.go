//go:build bend

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBendNativeShadowAgreement(t *testing.T) {
	evaluator := os.Getenv("TETHER_BEND_SHADOW")
	table, err := readBendTable(evaluator, eligibilityProtocol)
	if err != nil {
		t.Fatal(err)
	}
	store := testStore(t)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	log, err := openNudgeLog(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	for stateIndex, state := range []ThreadState{ThreadNew, ThreadNeedsReply, ThreadWaitingOnThem, ThreadFYI, ThreadNoise, ThreadDone} {
		for flags := 0; flags < 8; flags++ {
			for fires := 0; fires < 3; fires++ {
				thread := &Thread{ID: fmt.Sprintf("<%d-%d-%d@example.com>", stateIndex, flags, fires), State: state,
					LastInbound: now, LastOutbound: now}
				if flags&1 != 0 {
					thread.SnoozeUntil = now.Add(time.Hour)
				}
				if flags&4 != 0 {
					thread.LastInbound = now.Add(-7 * 24 * time.Hour)
					thread.LastOutbound = thread.LastInbound
				}
				rule := "needs_reply"
				if state == ThreadWaitingOnThem {
					rule = "bump"
				}
				for index := 0; index < fires; index++ {
					if err := log.append(Nudge{RuleID: rule, EntityID: thread.ID, FiredAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
						t.Fatal(err)
					}
				}
				if flags&2 != 0 {
					if err := log.append(Nudge{RuleID: rule, EntityID: thread.ID, FiredAt: now.Add(-time.Hour)}); err != nil {
						t.Fatal(err)
					}
				}
				eligible := len(collectNudges(&Store{Threads: []*Thread{thread}}, log, now, nil)) == 1
				index := stateIndex*24 + flags*3 + fires
				if (table[index] == '1') != eligible {
					t.Fatalf("native table mismatch: state=%s flags=%d fires=%d", state, flags, fires)
				}
				store.Threads = append(store.Threads, thread)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(store.dir, "bend-eligibility.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BendShadow: evaluator, DiscordChannel: "test"}
	decisions := productionBendEligibility(store, cfg, log, now)
	if decisions == nil {
		t.Fatal("native eligibility fell back to Go")
	}
	if !reflect.DeepEqual(collectNudges(store, log, now, nil), collectNudges(store, log, now, decisions)) {
		t.Fatal("production eligibility differs from Go")
	}
	out, err := RunBendShadow(store, evaluator, now)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status       string `json:"status"`
		Checked      int    `json:"threads_checked"`
		Mismatches   int    `json:"mismatches"`
		GoEligible   int    `json:"go_eligible"`
		BendEligible int    `json:"bend_eligible"`
		CasesSeen    []int  `json:"cases_seen"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "ok" || report.Checked != shadowTableSize || report.Mismatches != 0 {
		t.Fatalf("native shadow comparison failed: %s", out)
	}
	if report.GoEligible != 5 || report.BendEligible != 5 || len(report.CasesSeen) != 36 {
		t.Fatalf("native shadow coverage failed: %s", out)
	}
	for index := 1; index < len(report.CasesSeen); index++ {
		if report.CasesSeen[index] <= report.CasesSeen[index-1] {
			t.Fatalf("coverage cases must be sorted and unique: %v", report.CasesSeen)
		}
	}
	original := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	deliveryStore := testStore(t)
	deliveryNow := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)
	deliveryStore.Threads = []*Thread{
		{ID: "reply", State: ThreadNeedsReply, LastInbound: deliveryNow.Add(-7 * 24 * time.Hour)},
		{ID: "bump", State: ThreadWaitingOnThem, LastOutbound: deliveryNow.Add(-7 * 24 * time.Hour)},
	}
	if err := os.WriteFile(filepath.Join(deliveryStore.dir, "bend-eligibility.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunNudges(deliveryStore, cfg, deliveryNow); err != nil {
		t.Fatal(err)
	}
	if transport.sent != 2 {
		t.Fatalf("native production sent %d, want 2", transport.sent)
	}

}
