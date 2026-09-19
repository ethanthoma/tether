package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func triageShadowTestHelper(mode string) {
	if os.Getenv("TETHER_DISCORD_TOKEN") != "" || os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(7)
	}
	if mode == "timeout" {
		time.Sleep(time.Minute)
		return
	}
	if mode == "sensitiveerr" {
		fmt.Fprintln(os.Stderr, "private-body@example.com")
		os.Exit(2)
	}
	if mode == "oversized" {
		fmt.Print(strings.Repeat("x", 40000))
		return
	}
	var request struct {
		Version int                `json:"version"`
		Cases   []triageShadowCase `json:"cases"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(3)
	}
	if request.Version != 1 {
		os.Exit(4)
	}
	confidence := .9
	response := triageShadowResponse{Version: 1, Policy: "synthetic-obligations-v1", ModelSHA256: strings.Repeat("a", 64)}
	for _, item := range request.Cases {
		response.Results = append(response.Results, triageShadowResult{ID: item.ID, Label: "fyi", Confidence: &confidence, Status: "ok"})
	}
	switch mode {
	case "v2", "v2mixed", "v2queue":
		response.Policy = "reply-triage-v2"
		if mode == "v2mixed" {
			response.Results[0].Label = "abstain"
		}
		if mode == "v2queue" {
			for index, item := range request.Cases {
				if item.Messages[0].Body == "Unclear." {
					response.Results[index].Label = "abstain"
				}
			}
		}
	case "version":
		response.Version = 2
	case "policy":
		response.Policy = "different"
	case "hash":
		response.ModelSHA256 = "invalid"
	case "id":
		response.Results[0].ID = "unrequested"
	case "duplicate":
		response.Results = append(response.Results, response.Results[0])
	case "missing":
		response.Results = nil
	case "status":
		response.Results[0].Status = "unsure"
	case "label":
		response.Results[0].Label = "done"
	case "confidence":
		confidence = 1.1
	case "null":
		response.Results[0].Confidence = nil
	case "rejected":
		response.Results[0].Status = "input_rejected"
	case "validrejected":
		response.Results[0].Status = "input_rejected"
		response.Results[0].Label = "abstain"
		confidence = 0
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(5)
	}
}

func triageShadowHelper(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "triage-shadow-helper-"+mode)
	if err := os.Symlink(executable, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTriageShadowProtocol(t *testing.T) {
	t.Setenv("TETHER_DISCORD_TOKEN", "secret")
	t.Setenv("OPENAI_API_KEY", "secret")
	cases := []triageShadowCase{{ID: "case-1", Messages: []triageShadowMessage{{Body: "Private body"}}}}
	for _, mode := range []string{"good", "v2", "validrejected", "version", "policy", "hash", "id", "duplicate", "missing", "status", "label", "confidence", "null", "rejected", "oversized", "sensitiveerr", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			started := time.Now()
			response, err := readTriageShadow(ctx, triageShadowHelper(t, mode), cases)
			if mode == "good" || mode == "v2" || mode == "validrejected" {
				if err != nil || len(response.Results) != 1 {
					t.Fatalf("valid response: %+v, %v", response, err)
				}
			} else if err == nil {
				t.Fatalf("accepted invalid %s", mode)
			}
			if err != nil && strings.Contains(err.Error(), "private-body") {
				t.Fatal("stderr leaked")
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("timeout not enforced")
			}
		})
	}
	if _, err := readTriageShadow(context.Background(), "relative", cases); err == nil {
		t.Fatal("relative executable accepted")
	}
}

func triageShadowFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestTriageShadowReadOnlyAndPrivate(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for i, state := range []ThreadState{ThreadNeedsReply, ThreadNew, ThreadDone} {
		id := fmt.Sprintf("private-%d@example.com", i)
		message := &Message{MsgID: id, From: "sender@example.com", Body: "Private message body", Subject: "Private subject", Date: now}
		if err := store.SaveMessage(message); err != nil {
			t.Fatal(err)
		}
		store.Threads = append(store.Threads, &Thread{ID: id, State: state, Subject: message.Subject, MsgIDs: []string{id}, LastInbound: now})
	}
	store.Threads = append(store.Threads, &Thread{ID: "missing@example.com", State: ThreadNew, MsgIDs: []string{"missing"}, LastInbound: now})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	beforeFiles := triageShadowFiles(t, store.dir)
	beforeMemory, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RunTriageShadow(store, triageShadowHelper(t, "good"), now)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Mode          string `json:"mode"`
		PolicyAligned bool   `json:"policy_aligned"`
		Checked       int    `json:"checked"`
		Skipped       int    `json:"skipped"`
		Comparisons   int    `json:"comparisons"`
		Disagreements int    `json:"disagreements"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != "read_only" || report.PolicyAligned || report.Checked != 2 || report.Skipped != 2 || report.Comparisons != 1 || report.Disagreements != 1 {
		t.Fatalf("unexpected report: %s", output)
	}
	for _, private := range []string{"Private message", "Private subject", "@example.com", "missing@example.com"} {
		if strings.Contains(output, private) {
			t.Fatalf("report leaked %q", private)
		}
	}
	afterMemory, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeMemory) != string(afterMemory) || !reflect.DeepEqual(beforeFiles, triageShadowFiles(t, store.dir)) {
		t.Fatal("shadow changed store")
	}
}

func TestTriageShadowSelectsNewestBoundedThreads(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 23; i++ {
		id := fmt.Sprintf("thread-%d", i)
		if err := store.SaveMessage(&Message{MsgID: id, Body: "An update", Date: now.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
		store.Threads = append(store.Threads, &Thread{ID: id, State: ThreadNew, MsgIDs: []string{id}, LastInbound: now.Add(time.Duration(i) * time.Minute)})
	}
	output, err := RunTriageShadow(store, triageShadowHelper(t, "good"), now)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Checked     int `json:"checked"`
		Predictions []struct {
			ThreadHash string `json:"thread_hash"`
		} `json:"predictions"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Checked != 20 || len(report.Predictions) != 20 {
		t.Fatalf("unbounded selection: %s", output)
	}
	for index, prediction := range report.Predictions {
		sum := sha256.Sum256([]byte(store.Threads[22-index].ID))
		if prediction.ThreadHash != hex.EncodeToString(sum[:]) {
			t.Fatalf("prediction %d not newest-first: %+v", index, prediction)
		}
	}
}

func TestTriageShadowRejectsIncompleteOrOversizedContext(t *testing.T) {
	store := testStore(t)
	now := time.Now()
	for _, message := range []*Message{{MsgID: "good", Body: "Valid latest body"}, {MsgID: "huge", Body: strings.Repeat("x", 4001)}, {MsgID: "empty"}} {
		if err := store.SaveMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	for index, ids := range [][]string{{"missing", "good"}, {"huge"}, {"empty"}, {}} {
		store.Threads = append(store.Threads, &Thread{ID: fmt.Sprintf("invalid-%d", index), State: ThreadNew, MsgIDs: ids, LastInbound: now})
	}
	output, err := RunTriageShadow(store, triageShadowHelper(t, "good"), now)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Checked int `json:"checked"`
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Checked != 0 || report.Skipped != 4 {
		t.Fatalf("unsafe context accepted: %s", output)
	}
}

func TestTriageShadowUsesLastTwoMessagesWithoutTruncation(t *testing.T) {
	store := testStore(t)
	for _, message := range []*Message{{MsgID: "previous", Body: "Previous request", Outbound: true}, {MsgID: "latest", Body: strings.Repeat("é", 2000)}} {
		if err := store.SaveMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	thread := &Thread{MsgIDs: []string{"uncached-older", "previous", "latest"}}
	messages, err := triageShadowMessages(store, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Body != "Previous request" || !messages[0].Outbound || messages[1].Outbound || messages[1].Body != strings.Repeat("é", 2000) {
		t.Fatalf("message context changed: %+v", messages)
	}
}
