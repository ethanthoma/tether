//go:build bend

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBendFollowUpAgreement(t *testing.T) {
	var program strings.Builder
	program.WriteString(`import Base
import ./follow_up.bend as Model

def show(value: Bool) -> String:
  match value:
    case True{}:
      "true"
    case False{}:
      "false"

def is_new(state: Model.ThreadState) -> Bool:
  match state:
    case Model.New{}:
      True{}
    case Model.NeedsReply{}:
      False{}
    case Model.WaitingOnThem{}:
      False{}
    case Model.FYI{}:
      False{}
    case Model.Noise{}:
      False{}
    case Model.Done{}:
      False{}

def main() -> IO(Unit):
  do IO<Unit>:
`)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	booleans := map[bool]string{false: "False{}", true: "True{}"}
	type historyCase struct {
		count  int
		age    time.Duration
		recent bool
		fires  int
	}
	var expected []string
	eligibilityCases := 0
	for _, state := range []struct {
		goState ThreadState
		bend    string
	}{
		{ThreadNew, "New"}, {ThreadNeedsReply, "NeedsReply"},
		{ThreadWaitingOnThem, "WaitingOnThem"}, {ThreadFYI, "FYI"},
		{ThreadNoise, "Noise"}, {ThreadDone, "Done"},
	} {
		cooldown := replyCooldown
		rule := "needs_reply"
		if state.goState == ThreadWaitingOnThem {
			cooldown = bumpQuiet
			rule = "bump"
		}
		histories := []historyCase{{}}
		for count := 1; count <= 3; count++ {
			for _, boundary := range []struct {
				age    time.Duration
				recent bool
				active bool
			}{
				{cooldown - time.Nanosecond, true, true},
				{cooldown, false, true},
				{cooldown + time.Nanosecond, false, true},
				{nudgeLogMaxAge - time.Nanosecond, false, true},
				{nudgeLogMaxAge, false, false},
			} {
				fires := 0
				if boundary.active {
					fires = count
				}
				histories = append(histories, historyCase{count, boundary.age, boundary.recent, fires})
			}
		}
		for _, snoozed := range []bool{false, true} {
			for _, overdue := range []bool{false, true} {
				for _, history := range histories {
					thread := &Thread{ID: "<test@example.com>", State: state.goState,
						LastInbound: now.Add(-replyOverdue), LastOutbound: now.Add(-bumpQuiet)}
					if snoozed {
						thread.SnoozeUntil = now.Add(time.Hour)
					}
					if overdue {
						thread.LastInbound = thread.LastInbound.Add(-time.Nanosecond)
						thread.LastOutbound = thread.LastOutbound.Add(-time.Nanosecond)
					}
					log := &nudgeLog{}
					for index := 0; index < history.count; index++ {
						log.records = append(log.records, Nudge{RuleID: rule, EntityID: thread.ID, FiredAt: now.Add(-history.age)})
					}
					log.records = append(log.records,
						Nudge{RuleID: rule, EntityID: "<unrelated@example.com>", FiredAt: now},
						Nudge{RuleID: "unrelated_rule", EntityID: thread.ID, FiredAt: now})
					nudges := collectNudges(&Store{Threads: []*Thread{thread}}, log, now, nil)
					if len(nudges) > 1 {
						t.Fatalf("multiple nudges for one thread: %+v", nudges)
					}
					label := fmt.Sprintf("case:%s/%t/%t/%d/%d=", state.goState, snoozed, overdue, history.count, history.age)
					expected = append(expected, fmt.Sprintf("%s%t", label, len(nudges) == 1))
					fmt.Fprintf(&program, "    IO.print(%q ++ show(Model.can_nudge(%s, %s, Model.%s{}, %dn, %s)))\n",
						label, booleans[snoozed], booleans[history.recent], state.bend, history.fires, booleans[overdue])
					eligibilityCases++
				}
			}
		}
		thread := &Thread{State: state.goState}
		thread.ApplyOutbound(now)
		label := fmt.Sprintf("case:outbound/%s=", state.goState)
		expected = append(expected, fmt.Sprintf("%s%t", label, thread.State == ThreadNew))
		fmt.Fprintf(&program, "    IO.print(%q ++ show(is_new(Model.apply_outbound(Model.%s{}))))\n", label, state.bend)
	}
	bendCompare(t, "follow_up.bend", program.String(), expected)
	t.Logf("Go and Bend agree on %d eligibility cases and 6 outbound transitions", eligibilityCases)
}

func bendCompare(t *testing.T, modelName, program string, expected []string) {
	t.Helper()
	compiler := os.Getenv("BEND_MAIN")
	if compiler == "" {
		t.Fatal("run experiments/bend/check.sh to supply the pinned Bend compiler")
	}
	directory := t.TempDir()
	model, err := os.ReadFile(filepath.Join("experiments/bend", modelName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, modelName), model, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "agreement.bend")
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "bun", compiler, path).CombinedOutput()
	if err != nil {
		t.Fatalf("Bend comparison failed: %v\n%s", err, output)
	}
	var actual []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "case:") {
			actual = append(actual, line)
		}
	}
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("Go/Bend disagreement:\nGo:\n%s\nBend:\n%s", strings.Join(expected, "\n"), output)
	}
}
