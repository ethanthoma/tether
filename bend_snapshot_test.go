//go:build bend

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBendProductionSnapshot(t *testing.T) {
	snapshot := os.Getenv("TETHER_BEND_SNAPSHOT")
	if snapshot == "" {
		t.Skip("set TETHER_BEND_SNAPSHOT to an isolated production snapshot")
	}
	cfg := &Config{BendShadow: os.Getenv("TETHER_BEND_SHADOW"), DiscordChannel: "test", BendDispatch: os.Getenv("TETHER_BEND_DISPATCH")}
	if _, err := readBendTable(cfg.BendDispatch, dispatchProtocol); err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(time.Local)
	now = time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	var baseline []Nudge
	originalTransport := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	for _, mode := range []string{"go", "bend", "fallback", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			store := testStore(t)
			for _, name := range []string{"threads.json", "contacts.json", "commitments.json", "reminders.json", "calendar.json", "sync.json", "nudges.jsonl"} {
				data, err := os.ReadFile(filepath.Join(snapshot, name))
				if os.IsNotExist(err) && name != "threads.json" {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(store.dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, target := range store.files() {
				if err := loadJSON(filepath.Join(store.dir, name), target); err != nil {
					t.Fatal(err)
				}
			}
			productionCount := len(store.Threads)
			if productionCount == 0 || productionCount > shadowThreadsMax-8 {
				t.Fatal("snapshot must contain between 1 and 4088 production threads")
			}
			log, err := openNudgeLog(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			var fixtures []*Thread
			for _, state := range []ThreadState{ThreadNeedsReply, ThreadWaitingOnThem} {
				for _, condition := range []string{"eligible", "snoozed", "recent", "done"} {
					thread := &Thread{ID: "bend-staging-" + string(state) + "-" + condition, State: state,
						LastInbound: now.Add(-7 * 24 * time.Hour), LastOutbound: now.Add(-7 * 24 * time.Hour)}
					for _, existing := range store.Threads {
						if existing.ID == thread.ID {
							t.Fatal("fixture identifier collides with snapshot")
						}
					}
					switch condition {
					case "snoozed":
						thread.SnoozeUntil = now.Add(time.Hour)
					case "recent":
						rule := "needs_reply"
						if state == ThreadWaitingOnThem {
							rule = "bump"
						}
						if err := log.append(Nudge{RuleID: rule, EntityID: thread.ID, FiredAt: now.Add(-24 * time.Hour)}); err != nil {
							t.Fatal(err)
						}
					case "done":
						thread.State = ThreadDone
					}
					fixtures = append(fixtures, thread)
				}
			}
			store.Threads = append(fixtures, store.Threads...)
			marker := filepath.Join(store.dir, "bend-eligibility.enabled")
			dispatchMarker := filepath.Join(store.dir, "bend-dispatch.enabled")
			if mode != "go" {
				if err := os.WriteFile(dispatchMarker, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "rollback" {
				if err := os.Remove(dispatchMarker); err != nil {
					t.Fatal(err)
				}
				if productionBendEligibility(store, cfg, log, now) == nil {
					t.Fatal("Bend must work before rollback")
				}
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
			}
			candidate := *cfg
			if mode == "fallback" {
				candidate.BendShadow = filepath.Join(store.dir, "missing-evaluator")
				candidate.BendDispatch = candidate.BendShadow
			}
			decisions := productionBendEligibility(store, &candidate, log, now)
			if (decisions != nil) != (mode == "bend") {
				t.Fatal("unexpected eligibility backend")
			}
			if mode == "bend" {
				for index, thread := range fixtures {
					if decisions[thread] != (index%4 == 0) {
						t.Fatal("incorrect controlled reply/bump eligibility")
					}
				}
			}
			for _, commitment := range store.Commitments {
				commitment.Slip(now)
			}
			nudges := collectNudges(store, log, now, decisions)
			if mode == "go" {
				baseline = nudges
			} else if !reflect.DeepEqual(baseline, nudges) {
				t.Fatal("candidate decisions or notification contents differ from Go")
			}
			report, err := RunBendShadow(store, cfg.BendShadow, now)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Status   string `json:"status"`
				Eligible int    `json:"bend_eligible"`
			}
			if err := json.Unmarshal([]byte(report), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != "ok" || result.Eligible < 2 {
				t.Fatal("controlled snapshot shadow gate failed")
			}
			dispatchReport, err := RunBendDispatchShadow(store, cfg.BendDispatch, now)
			if err != nil {
				t.Fatal(err)
			}
			var dispatchResult struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(dispatchReport), &dispatchResult); err != nil {
				t.Fatal(err)
			}
			if dispatchResult.Status != "ok" {
				t.Fatal("snapshot dispatch disagreement")
			}
			t.Log(dispatchReport)
			before := len(log.records)
			allowance := max(0, maxPushesPerDay-log.firedToday("", now))
			*transport = bendDiscordTransport{}
			if err := RunNudges(store, &candidate, now); err != nil {
				t.Fatal(err)
			}
			persisted, err := openNudgeLog(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			want := min(allowance, len(nudges))
			if transport.sent != want || len(persisted.records) != before+want {
				t.Fatal("delivery or persistence differs from daily allowance")
			}
			for index, nudge := range persisted.records[before:] {
				if nudge.EntityID != nudges[index].EntityID || nudge.RuleID != nudges[index].RuleID || !nudge.FiredAt.Equal(now) {
					t.Fatal("confirmed delivery log differs from selected notification")
				}
			}
			t.Logf("production_threads=%d controlled_threads=%d fake_deliveries=%d %s", productionCount, len(fixtures), transport.sent, report)
		})
	}
}
