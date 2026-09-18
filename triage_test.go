package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOutboundRequiresTriage(t *testing.T) {
	for _, state := range []ThreadState{ThreadNew, ThreadNeedsReply, ThreadWaitingOnThem, ThreadFYI, ThreadNoise, ThreadDone} {
		t.Run(string(state), func(t *testing.T) {
			now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
			thread := &Thread{State: state, TriageNote: "old request", TriageAttempts: triageMaxAttempts}
			thread.ApplyOutbound(now)
			if thread.State != ThreadNew {
				t.Fatalf("sent message must await classification, got %s", thread.State)
			}
			if thread.TriageNote != "" || thread.TriageAttempts != 0 {
				t.Fatalf("new message retained stale triage: %+v", thread)
			}
			if !thread.LastOutbound.Equal(now) {
				t.Fatalf("outbound timestamp: %v", thread.LastOutbound)
			}
		})
	}
}

func TestOutboundFollowUpUsesVerdict(t *testing.T) {
	for _, example := range []struct {
		name  string
		body  string
		state ThreadState
	}{
		{"closing_reply", "Thanks, that resolves everything!", ThreadFYI},
		{"request", "Could you send the revised proposal?", ThreadWaitingOnThem},
	} {
		t.Run(example.name, func(t *testing.T) {
			store := testStore(t)
			sent := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
			thread := &Thread{ID: "<root@example.com>", State: ThreadNeedsReply, Subject: "Proposal"}
			store.Threads = append(store.Threads, thread)
			message := &Message{MsgID: "<reply@example.com>", From: "me@example.com",
				To: []string{"bob@example.com"}, Date: sent, Outbound: true, Body: example.body}
			if err := store.SaveMessage(message); err != nil {
				t.Fatal(err)
			}
			applyMessage(store, map[string]*Thread{thread.ID: thread}, message, []string{thread.ID}, "me@example.com")
			now := sent.Add(6 * 24 * time.Hour)
			if nudges := collectNudges(store, &nudgeLog{}, now, nil); len(nudges) != 0 {
				t.Fatalf("unclassified sent message produced nudges: %+v", nudges)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct {
					Messages []struct{ Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				if len(request.Messages) < 2 {
					t.Error("missing triage context")
				} else if !strings.Contains(request.Messages[1].Content, example.body) {
					t.Error("sent message missing from triage context")
				}
				verdict, err := json.Marshal(triageVerdict{State: example.state, Note: "classified sent message"})
				if err != nil {
					t.Error(err)
					return
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"choices": []any{
					map[string]any{"message": map[string]string{"content": string(verdict)}},
				}}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			if err := RunTriage(store, &Config{MyEmail: "me@example.com", LLMURL: server.URL}, now); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || thread.State != example.state {
				t.Fatalf("triage calls=%d, state=%s; want %s", calls, thread.State, example.state)
			}
			nudges := collectNudges(store, &nudgeLog{}, now, nil)
			if example.state == ThreadFYI {
				if len(nudges) != 0 {
					t.Fatalf("closing reply produced nudges: %+v", nudges)
				}
			} else if len(nudges) != 1 || nudges[0].RuleID != "bump" {
				t.Fatalf("classified request must remain eligible for a bump: %+v", nudges)
			}
		})
	}
}

func TestTriageServiceFailurePreservesQueue(t *testing.T) {
	for _, status := range []int{0, 401, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"state\":\"fyi\"}"}}]}`)
			}))
			defer server.Close()
			if status == 0 {
				server.Close()
			}
			store := testStore(t)
			store.Threads = []*Thread{{ID: "first", State: ThreadNew, TriageAttempts: 2, TriageNote: "keep"}, {ID: "second", State: ThreadNew}}
			for run := 0; run < 4; run++ {
				if err := RunTriage(store, &Config{LLMURL: server.URL}, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if store.Threads[0].State != ThreadNew || store.Threads[0].TriageAttempts != 2 || store.Threads[0].TriageNote != "keep" || store.Threads[1].TriageAttempts != 0 {
				t.Fatalf("service outage changed queue: %+v %+v", store.Threads[0], store.Threads[1])
			}
			if status != 0 && calls != 4 {
				t.Fatalf("calls=%d, want one per run", calls)
			}
			recovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"state\":\"needs_reply\",\"note\":\"reply owed\"}"}}]}`)
			}))
			defer recovered.Close()
			if err := RunTriage(store, &Config{LLMURL: recovered.URL}, time.Now()); err != nil {
				t.Fatal(err)
			}
			for _, thread := range store.Threads {
				if thread.State != ThreadNeedsReply || thread.TriageAttempts != 0 {
					t.Fatalf("recovery failed: %+v", thread)
				}
			}
		})
	}
}

func TestTriageInvalidOutputStillExhaustsAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"invalid verdict"}}]}`)
	}))
	defer server.Close()
	store := testStore(t)
	thread := &Thread{ID: "invalid", State: ThreadNew}
	store.Threads = []*Thread{thread}
	for run := 0; run < triageMaxAttempts; run++ {
		if err := RunTriage(store, &Config{LLMURL: server.URL}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if thread.State != ThreadFYI || thread.TriageAttempts != triageMaxAttempts {
		t.Fatalf("invalid output did not exhaust attempts: %+v", thread)
	}
}
