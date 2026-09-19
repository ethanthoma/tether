package main

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeliveryShadowNeverControlsSending(t *testing.T) {
	originalTransport, originalLog := http.DefaultTransport, log.Writer()
	t.Cleanup(func() { http.DefaultTransport = originalTransport; log.SetOutput(originalLog) })
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	for _, mode := range []string{"model", "good", "state", "error", "version", "decision", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			log.SetOutput(&output)
			transport := &bendDiscordTransport{}
			http.DefaultTransport = transport
			store := testStore(t)
			store.Reminders = []*Reminder{{ID: "first", State: ReminderOpen, Due: now}, {ID: "second", State: ReminderOpen, Due: now}}
			cfg := &Config{DiscordChannel: "test", BendDelivery: shadowHelper(t, "delivery-"+mode)}
			if err := RunNudges(store, cfg, now); err != nil {
				t.Fatal(err)
			}
			nl, err := openNudgeLog(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if transport.sent != 2 || len(nl.records) != 2 {
				t.Fatal("observer affected sending or confirmations")
			}
			expected := "delivery shadow: unavailable"
			if mode == "model" {
				expected = `"mismatches":0,"confirmed":2,"failed":false`
			}
			if mode == "good" {
				expected = `"status":"mismatch"`
			}
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("missing %q in %s", expected, output.String())
			}
		})
	}
}

type deliveryLogFailureTransport struct {
	base bendDiscordTransport
	path string
}

func (transport *deliveryLogFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if err := os.Mkdir(transport.path, 0700); err != nil {
		return response, err
	}
	return response, nil
}

func TestDeliveryShadowStopsAtLogFailure(t *testing.T) {
	originalTransport, originalLog := http.DefaultTransport, log.Writer()
	t.Cleanup(func() { http.DefaultTransport = originalTransport; log.SetOutput(originalLog) })
	var output bytes.Buffer
	log.SetOutput(&output)
	store := testStore(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	store.Reminders = []*Reminder{{ID: "first", State: ReminderOpen, Due: now}, {ID: "second", State: ReminderOpen, Due: now}}
	if err := os.WriteFile(filepath.Join(store.dir, "bend-delivery.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	transport := &deliveryLogFailureTransport{path: filepath.Join(store.dir, "nudges.jsonl")}
	http.DefaultTransport = transport
	cfg := &Config{DiscordChannel: "test", BendDelivery: shadowHelper(t, "delivery-model")}
	err := RunNudges(store, cfg, now)
	if err == nil || !strings.Contains(err.Error(), "nudges: append") {
		t.Fatalf("expected log write failure, got %v", err)
	}
	if transport.base.attempts != 1 {
		t.Fatal("sent again after log failure")
	}
	if !strings.Contains(output.String(), `"mismatches":0,"confirmed":0,"failed":true`) {
		t.Fatalf("incorrect failed persistence observation: %s", output.String())
	}
}

func TestDeliveryShadowSkipsIdleBatch(t *testing.T) {
	originalTransport, originalLog := http.DefaultTransport, log.Writer()
	t.Cleanup(func() { http.DefaultTransport = originalTransport; log.SetOutput(originalLog) })
	var output bytes.Buffer
	log.SetOutput(&output)
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	store := testStore(t)
	cfg := &Config{DiscordChannel: "test", BendDelivery: "relative-path-must-not-be-invoked"}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	if err := RunNudges(store, cfg, now); err != nil {
		t.Fatal(err)
	}
	store.Reminders = []*Reminder{{ID: "test", State: ReminderOpen, Due: now}}
	if err := RunNudges(store, cfg, now.Add(11*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if transport.attempts != 0 || output.Len() != 0 {
		t.Fatal("idle or quiet batch invoked observer or sender")
	}
}

func TestBendDeliveryActivationFallbackAndRollback(t *testing.T) {
	for _, mode := range []string{"model", "good", "state", "error", "version", "decision", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			store := testStore(t)
			path := shadowHelper(t, "delivery-"+mode)
			marker := filepath.Join(store.dir, "bend-delivery.enabled")
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			policy := openDeliveryPolicy(path, store.dir)
			if policy.active != (mode == "model") {
				t.Fatalf("unexpected activation for %s", mode)
			}
			for _, succeeded := range []bool{true, false, true} {
				policy.observe(succeeded)
			}
			if policy.state != (deliveryState{confirmed: 1, failed: true}) {
				t.Fatalf("unsafe failure transition: %+v", policy.state)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if openDeliveryPolicy(path, store.dir).active {
				t.Fatal("rollback retained authority")
			}
		})
	}
}

func TestBendDeliveryRejectsInvalidSwitch(t *testing.T) {
	store := testStore(t)
	marker := filepath.Join(store.dir, "bend-delivery.enabled")
	for _, kind := range []string{"nonempty", "directory", "symlink"} {
		switch kind {
		case "nonempty":
			if err := os.WriteFile(marker, []byte("yes"), 0600); err != nil {
				t.Fatal(err)
			}
		case "directory":
			if err := os.Mkdir(marker, 0700); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Symlink(filepath.Join(store.dir, "missing"), marker); err != nil {
				t.Fatal(err)
			}
		}
		if openDeliveryPolicy(shadowHelper(t, "delivery-model"), store.dir).active {
			t.Fatalf("accepted %s switch", kind)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
}
