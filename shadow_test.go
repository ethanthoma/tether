package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(tests *testing.M) {
	mode := filepath.Base(os.Args[0])
	if strings.HasPrefix(mode, "shadow-helper-") {
		protocol := eligibilityProtocol
		if strings.HasPrefix(mode, "shadow-helper-dispatch-") {
			protocol = dispatchProtocol
			mode = strings.Replace(mode, "dispatch-", "", 1)
		}
		output := protocol.prefix + strings.Repeat("0", protocol.size) + "\n"
		switch mode {
		case "shadow-helper-good":
			if len(os.Environ()) != 0 {
				os.Exit(3)
			}
		case "shadow-helper-five":
			output = dispatchProtocol.prefix + strings.Repeat("5", dispatchProtocol.size) + "\n"
		case "shadow-helper-error":
			os.Exit(2)
		case "shadow-helper-version":
			output = strings.Replace(output, "v1", "v9", 1)
		case "shadow-helper-decision":
			output = protocol.prefix + strings.Repeat("x", protocol.size) + "\n"
		case "shadow-helper-overflow":
			output += strings.Repeat("extra", 10000)
		case "shadow-helper-timeout":
			time.Sleep(2 * shadowTimeout)
		default:
			os.Exit(4)
		}
		if _, err := fmt.Print(output); err != nil {
			os.Exit(5)
		}
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func shadowHelper(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shadow-helper-"+mode)
	if err := os.Symlink(executable, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShadowProtocol(t *testing.T) {
	t.Setenv("TETHER_DISCORD_TOKEN", "must-not-reach-evaluator")
	for _, mode := range []string{"good", "error", "version", "decision", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			started := time.Now()
			table, err := readBendTable(shadowHelper(t, mode), eligibilityProtocol)
			if mode == "good" {
				if err != nil || len(table) != shadowTableSize {
					t.Fatalf("valid evaluator: table=%q error=%v", table, err)
				}
			} else if err == nil {
				t.Fatalf("accepted invalid evaluator %s", mode)
			}
			if time.Since(started) > shadowTimeout+time.Second {
				t.Fatal("evaluator exceeded its timeout allowance")
			}
		})
	}
	if _, err := readBendTable("relative-path", eligibilityProtocol); err == nil {
		t.Fatal("relative evaluator path accepted")
	}
}

func TestShadowReportsMismatchWithoutWriting(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	thread := &Thread{ID: "<private@example.com>", Subject: "Private subject", State: ThreadNeedsReply,
		LastInbound: now.Add(-72 * time.Hour)}
	store.Threads = []*Thread{thread}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(store.dir, "threads.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunBendShadow(store, shadowHelper(t, "good"), now)
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
	if report.Status != "mismatch" || report.Checked != 1 || report.Mismatches != 1 {
		t.Fatalf("incorrect mismatch report: %s", out)
	}
	if report.GoEligible != 1 || report.BendEligible != 0 || len(report.CasesSeen) != 1 || report.CasesSeen[0] != 36 {
		t.Fatalf("incorrect policy coverage: %s", out)
	}
	if strings.Contains(out, thread.Subject) || strings.Contains(out, thread.ID) {
		t.Fatal("report exposed message content or full identifiers")
	}
	after, err := os.ReadFile(filepath.Join(store.dir, "threads.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("shadow modified thread storage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "nudges.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("shadow created a notification log: %v", err)
	}
}
