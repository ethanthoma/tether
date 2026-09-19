package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTriageNativeProduction(t *testing.T) {
	evaluator := os.Getenv("TETHER_TRIAGE_NATIVE")
	if evaluator == "" {
		t.Skip("set TETHER_TRIAGE_NATIVE, TETHER_TRIAGE_NATIVE_BINARY, and TETHER_TRIAGE_NATIVE_HASH")
	}
	binary, approved := os.Getenv("TETHER_TRIAGE_NATIVE_BINARY"), os.Getenv("TETHER_TRIAGE_NATIVE_HASH")
	if !filepath.IsAbs(evaluator) || !filepath.IsAbs(binary) {
		t.Fatal("native evaluator and bare tether binary must be absolute paths")
	}
	if _, err := hex.DecodeString(approved); err != nil || len(approved) != 64 || approved != strings.ToLower(approved) {
		t.Fatal("native artifact hash must contain 64 lowercase hexadecimal characters")
	}
	type nativeCase struct {
		Expected string                `json:"expected"`
		Messages []triageShadowMessage `json:"messages"`
	}
	packet := struct {
		Provenance string       `json:"provenance"`
		Cases      []nativeCase `json:"cases"`
	}{"fully_synthetic", []nativeCase{{"abstain", []triageShadowMessage{{Body: "Could you confirm whether the workshop starts at nine?"}}}}}
	if path := os.Getenv("TETHER_TRIAGE_NATIVE_CASES"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 256*1024+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read synthetic packet: %v / %v", readErr, closeErr)
		}
		if len(data) > 256*1024 {
			t.Fatal("synthetic native packet exceeds input bound")
		}
		packet.Provenance, packet.Cases = "", nil
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatal(err)
		}
	}
	if packet.Provenance != "fully_synthetic" || len(packet.Cases) < 1 || len(packet.Cases) > triageMaxPerRun {
		t.Fatal("native smoke requires 1–20 declared synthetic cases")
	}
	accepted, abstained := 0, 0
	for _, example := range packet.Cases {
		switch example.Expected {
		case "abstain":
			abstained++
		case string(ThreadNeedsReply), string(ThreadWaitingOnThem), string(ThreadFYI), string(ThreadNoise):
			accepted++
		default:
			t.Fatal("invalid synthetic expected label")
		}
		if len(example.Messages) < 1 || len(example.Messages) > 2 {
			t.Fatal("synthetic case must contain one or two messages")
		}
		for _, message := range example.Messages {
			if len(message.Body) == 0 || len(message.Body) > triageMsgChars {
				t.Fatal("synthetic message outside byte bound")
			}
		}
	}
	if abstained == 0 {
		t.Fatal("native packet must exercise abstention")
	}
	if os.Getenv("TETHER_TRIAGE_NATIVE_CASES") != "" && accepted == 0 {
		t.Fatal("approval smoke packet must also exercise accepted classifications")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	for _, mode := range []string{"active", "wrong_hash", "absent", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			store, err := OpenStore(directory)
			if err != nil {
				t.Fatal(err)
			}
			for index, example := range packet.Cases {
				thread := &Thread{ID: fmt.Sprintf("synthetic-native-%d", index), State: ThreadNew, TriageAttempts: 2, TriageNote: "pending"}
				for messageIndex, message := range example.Messages {
					id := fmt.Sprintf("synthetic-native-%d-%d", index, messageIndex)
					if err := store.SaveMessage(&Message{MsgID: id, Body: message.Body, Outbound: message.Outbound}); err != nil {
						t.Fatal(err)
					}
					thread.MsgIDs = append(thread.MsgIDs, id)
				}
				store.Threads = append(store.Threads, thread)
			}
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(directory, "triage-model.enabled")
			if mode != "absent" {
				value := approved
				if mode == "wrong_hash" {
					value = strings.Repeat("0", 64)
					if value == approved {
						value = strings.Repeat("1", 64)
					}
				}
				if err := os.WriteFile(marker, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "rollback" {
					if err := os.Remove(marker); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "triage")
			command.Env = []string{"TETHER_STATE_DIR=" + directory, "TETHER_TRIAGE_SHADOW=" + evaluator, "TETHER_LLM_URL=" + server.URL}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("native triage failed: %v: %s", err, output)
			}
			if mode == "active" && !strings.Contains(string(output), "triage: v2 model active") {
				t.Fatalf("native model did not activate: %s", output)
			}
			if mode == "wrong_hash" && !strings.Contains(string(output), "unapproved model identity or policy") {
				t.Fatalf("hash mismatch was not rejected: %s", output)
			}
			persisted, err := OpenStore(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer persisted.Close()
			if len(persisted.Threads) != len(packet.Cases) {
				t.Fatal("native triage changed the number of threads")
			}
			for index, thread := range persisted.Threads {
				expected, attempts := ThreadNew, 2
				if mode == "active" && packet.Cases[index].Expected != "abstain" {
					expected, attempts = ThreadState(packet.Cases[index].Expected), 0
				}
				if thread.State != expected || thread.TriageAttempts != attempts {
					t.Fatalf("case %d: state=%s attempts=%d; want %s/%d", index, thread.State, thread.TriageAttempts, expected, attempts)
				}
				if expected == ThreadNew && thread.TriageNote != "pending" {
					t.Fatal("undecided thread note changed during LLM outage")
				}
			}
			if len(persisted.Commitments) != 0 {
				t.Fatal("native triage unexpectedly extracted commitments")
			}
		})
	}
	t.Logf("native smoke: cases=%d accepted=%d abstained=%d; activation/fallback/rollback passed", len(packet.Cases), accepted, abstained)
}
