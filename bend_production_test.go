package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type bendDiscordTransport struct {
	sent      int
	attempts  int
	failureAt int
	status    int
}

func (transport *bendDiscordTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		defer request.Body.Close()
	}
	if request.Method != http.MethodPost || request.URL.Host != "discord.com" || request.URL.Path != "/api/v10/channels/test/messages" {
		return nil, fmt.Errorf("unexpected Discord request: %s %s", request.Method, request.URL)
	}
	transport.attempts++
	if transport.attempts > 8 {
		return nil, fmt.Errorf("unexpectedly large notification batch")
	}
	if transport.attempts == transport.failureAt {
		if transport.status == 0 {
			return nil, fmt.Errorf("simulated rejection")
		}
		return &http.Response{StatusCode: transport.status,
			Status: fmt.Sprintf("%d %s", transport.status, http.StatusText(transport.status)),
			Header: make(http.Header), Body: io.NopCloser(strings.NewReader("simulated rejection")), Request: request}, nil
	}
	transport.sent++
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"id":"test"}`)), Request: request}, nil
}

func TestBendProductionSwitch(t *testing.T) {
	original := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)
	for _, mode := range []string{"good", "error", "version", "decision", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			store := testStore(t)
			store.Threads = []*Thread{
				{ID: "reply", State: ThreadNeedsReply, LastInbound: now.Add(-7 * 24 * time.Hour)},
				{ID: "bump", State: ThreadWaitingOnThem, LastOutbound: now.Add(-7 * 24 * time.Hour)},
			}
			cfg := &Config{BendShadow: shadowHelper(t, mode), DiscordChannel: "test"}
			marker := filepath.Join(store.dir, "bend-eligibility.enabled")
			if got := productionBendEligibility(store, cfg, &nudgeLog{}, now); got != nil {
				t.Fatal("enabled without switch")
			}
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			transport.sent = 0
			transport.attempts = 0
			if err := RunNudges(store, cfg, now); err != nil {
				t.Fatal(err)
			}
			want := 2
			if mode == "good" {
				want = 0
			}
			if transport.sent != want {
				t.Fatalf("sent %d, want %d", transport.sent, want)
			}
			nl, err := openNudgeLog(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(nl.records) != want {
				t.Fatalf("logged %d, want %d", len(nl.records), want)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if got := productionBendEligibility(store, cfg, nl, now); got != nil {
				t.Fatal("switch removal did not restore Go")
			}
			if mode == "good" {
				if err := RunNudges(store, cfg, now); err != nil {
					t.Fatal(err)
				}
				if transport.sent != 2 {
					t.Fatal("Go did not resume sending after switch removal")
				}
			}
		})
	}
}

func TestBendProductionFallbackBounds(t *testing.T) {
	now := time.Now()
	for _, scenario := range []string{"directory", "nonempty", "unknown-state", "thread-limit", "missing-executable"} {
		t.Run(scenario, func(t *testing.T) {
			store := testStore(t)
			marker := filepath.Join(store.dir, "bend-eligibility.enabled")
			cfg := &Config{BendShadow: shadowHelper(t, "good")}
			if scenario == "directory" {
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				var content []byte
				if scenario == "nonempty" {
					content = []byte("unexpected")
				}
				if err := os.WriteFile(marker, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "unknown-state":
				store.Threads = []*Thread{{State: ThreadState("invalid")}}
			case "thread-limit":
				store.Threads = make([]*Thread, shadowThreadsMax+1)
			case "missing-executable":
				cfg.BendShadow = filepath.Join(store.dir, "missing")
			}
			if got := productionBendEligibility(store, cfg, &nudgeLog{}, now); got != nil {
				t.Fatal("expected whole-batch Go fallback")
			}
		})
	}
}
