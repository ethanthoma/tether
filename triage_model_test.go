package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func productionTriageStore(t *testing.T, count int) *Store {
	t.Helper()
	store := testStore(t)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("model-%d", i)
		if err := store.SaveMessage(&Message{MsgID: id, Body: "Please reply with your availability."}); err != nil {
			t.Fatal(err)
		}
		store.Threads = append(store.Threads, &Thread{ID: id, State: ThreadNew, MsgIDs: []string{id}, TriageAttempts: 2, TriageNote: "pending"})
	}
	return store
}

func productionTriageSwitch(t *testing.T, store *Store, value string) string {
	t.Helper()
	path := filepath.Join(store.dir, "triage-model.enabled")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProductionTriageRequiresPinnedV2AndSupportsRollback(t *testing.T) {
	for _, example := range []struct {
		name, mode, hash string
		accepted         bool
	}{
		{"v2", "v2", strings.Repeat("a", 64), true},
		{"v2_newline", "v2", strings.Repeat("a", 64) + "\n", true},
		{"missing", "v2", "", false},
		{"short", "v2", "bad", false},
		{"nonhex", "v2", strings.Repeat("z", 64), false},
		{"oversized", "v2", strings.Repeat("a", 66), false},
		{"different_hash", "v2", strings.Repeat("b", 64), false},
		{"v1", "good", strings.Repeat("a", 64), false},
		{"unavailable", "sensitiveerr", strings.Repeat("a", 64), false},
	} {
		t.Run(example.name, func(t *testing.T) {
			store := productionTriageStore(t, 1)
			if example.hash != "" {
				productionTriageSwitch(t, store, example.hash)
			}
			evaluator := triageShadowHelper(t, example.mode)
			candidates := triageCandidates(store, time.Unix(0, 0), false)
			predicted := productionTriagePredictions(store, evaluator, productionTriageHash(store), candidates)
			if example.accepted {
				if len(predicted) != 1 || predicted[store.Threads[0]] != ThreadFYI {
					t.Fatalf("approved model rejected: %+v", predicted)
				}
				if err := os.Remove(filepath.Join(store.dir, "triage-model.enabled")); err != nil {
					t.Fatal(err)
				}
				if len(productionTriagePredictions(store, evaluator, productionTriageHash(store), candidates)) != 0 {
					t.Fatal("rollback retained model activation")
				}
			} else if len(predicted) != 0 {
				t.Fatalf("unapproved model accepted: %+v", predicted)
			}
			if store.Threads[0].State != ThreadNew || store.Threads[0].TriageAttempts != 2 {
				t.Fatal("prediction changed thread")
			}
		})
	}
	store := productionTriageStore(t, 1)
	target := filepath.Join(t.TempDir(), "approval")
	if err := os.WriteFile(target, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(store.dir, "triage-model.enabled")); err != nil {
		t.Fatal(err)
	}
	if len(productionTriagePredictions(store, triageShadowHelper(t, "v2"), productionTriageHash(store), triageCandidates(store, time.Unix(0, 0), false))) != 0 {
		t.Fatal("symlink approval accepted")
	}
}

func TestProductionTriageContinuesAfterAbstentionDuringLLMOutage(t *testing.T) {
	store := productionTriageStore(t, 2)
	productionTriageSwitch(t, store, strings.Repeat("a", 64))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	if err := RunTriage(store, &Config{LLMURL: server.URL, TriageShadow: triageShadowHelper(t, "v2mixed")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	first, second := store.Threads[0], store.Threads[1]
	if calls != 1 || first.State != ThreadNew || first.TriageAttempts != 2 || first.TriageNote != "pending" {
		t.Fatalf("abstention damaged queue: calls=%d thread=%+v", calls, first)
	}
	if second.State != ThreadFYI || second.TriageAttempts != 0 || second.TriageNote == "pending" {
		t.Fatalf("LLM outage blocked accepted local prediction: %+v", second)
	}
}

func TestProductionTriageAbstentionPreservesCommitmentFallback(t *testing.T) {
	store := productionTriageStore(t, 2)
	productionTriageSwitch(t, store, strings.Repeat("a", 64))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"state\":\"fyi\",\"commitments\":[{\"text\":\"Send the draft\",\"due\":\"2026-10-01\"}]}"}}]}`)
	}))
	defer server.Close()
	if err := RunTriage(store, &Config{LLMURL: server.URL, TriageShadow: triageShadowHelper(t, "v2mixed")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(store.Commitments) != 1 || store.Commitments[0].Text != "Send the draft" || store.Commitments[0].ThreadID != store.Threads[0].ID {
		t.Fatalf("fallback lost commitment: calls=%d commitments=%+v", calls, store.Commitments)
	}
	for _, thread := range store.Threads {
		if thread.State != ThreadFYI || thread.TriageAttempts != 0 {
			t.Fatalf("classification failed: %+v", thread)
		}
	}
}

func TestTriageCandidateRotationIsBoundedAndKeepsLegacyOrder(t *testing.T) {
	store := productionTriageStore(t, 43)
	store.Threads[2].State = ThreadDone
	seen := map[*Thread]bool{}
	for window := int64(0); window < 3; window++ {
		candidates := triageCandidates(store, time.Unix(window*900, 0), true)
		if len(candidates) != triageMaxPerRun {
			t.Fatalf("window %d selected %d candidates", window, len(candidates))
		}
		unique := map[*Thread]bool{}
		for _, thread := range candidates {
			if unique[thread] || thread.State != ThreadNew {
				t.Fatal("duplicate or ineligible candidate")
			}
			unique[thread], seen[thread] = true, true
		}
		stable := triageCandidates(store, time.Unix(window*900+899, 0), true)
		for index, thread := range candidates {
			if stable[index] != thread {
				t.Fatal("candidate order changed within one window")
			}
		}
	}
	if len(seen) != 42 {
		t.Fatalf("starved %d candidates", 42-len(seen))
	}
	legacy := triageCandidates(store, time.Unix(900, 0), false)
	if legacy[0] != store.Threads[0] || legacy[2] != store.Threads[3] || legacy[19] != store.Threads[20] {
		t.Fatal("disabled model changed original queue order")
	}
}

func TestProductionTriageRotatesPastOfflineAbstentions(t *testing.T) {
	store := productionTriageStore(t, 25)
	for _, thread := range store.Threads[:20] {
		if err := store.SaveMessage(&Message{MsgID: thread.MsgIDs[0], Body: "Unclear."}); err != nil {
			t.Fatal(err)
		}
	}
	productionTriageSwitch(t, store, strings.Repeat("a", 64))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := &Config{LLMURL: server.URL, TriageShadow: triageShadowHelper(t, "v2queue")}
	for window := int64(0); window < 2; window++ {
		if err := RunTriage(store, cfg, time.Unix(window*900, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("expected one LLM availability check per run, got %d", calls)
	}
	for index, thread := range store.Threads {
		if index < 20 {
			if thread.State != ThreadNew || thread.TriageAttempts != 2 || thread.TriageNote != "pending" {
				t.Fatalf("abstention changed queued thread: %+v", thread)
			}
		} else if thread.State != ThreadFYI || thread.TriageAttempts != 0 {
			t.Fatalf("later model-accepted thread starved: %+v", thread)
		}
	}
}
