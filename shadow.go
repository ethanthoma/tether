package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	shadowPrefix     = "tether-bend-shadow-v1:"
	shadowTableSize  = 144
	shadowThreadsMax = 4096
	shadowLogMax     = 8 << 20
	shadowTimeout    = 2 * time.Second
)

type bendProtocol struct {
	prefix string
	size   int
	digits string
}

var eligibilityProtocol = bendProtocol{shadowPrefix, shadowTableSize, "01"}
var dispatchProtocol = bendProtocol{"tether-bend-dispatch-v1:", 72, "012345"}

type shadowOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (output *shadowOutput) Write(data []byte) (int, error) {
	if output.buffer.Len()+len(data) > output.limit {
		return 0, fmt.Errorf("shadow: evaluator output exceeds protocol limit")
	}
	return output.buffer.Write(data)
}

func readBendTable(path string, protocol bendProtocol) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("shadow: evaluator must name an absolute executable path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), shadowTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	command.Env = []string{}
	command.Stderr = io.Discard
	command.WaitDelay = 100 * time.Millisecond
	output := shadowOutput{limit: len(protocol.prefix) + protocol.size + 1}
	command.Stdout = &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("shadow: evaluator timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("shadow: evaluator failed: %w", err)
	}
	raw := output.buffer.String()
	if len(raw) != len(protocol.prefix)+protocol.size+1 || !strings.HasPrefix(raw, protocol.prefix) || !strings.HasSuffix(raw, "\n") {
		return nil, fmt.Errorf("shadow: invalid evaluator protocol")
	}
	table := raw[len(protocol.prefix) : len(raw)-1]
	if strings.Trim(table, protocol.digits) != "" {
		return nil, fmt.Errorf("shadow: invalid evaluator decision")
	}
	return []byte(table), nil
}

func RunBendShadow(store *Store, evaluator string, now time.Time) (string, error) {
	started := time.Now()
	table, err := readBendTable(evaluator, eligibilityProtocol)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(filepath.Join(store.dir, "nudges.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("shadow: stat nudge log: %w", err)
	}
	if info != nil && info.Size() > shadowLogMax {
		return "", fmt.Errorf("shadow: nudge log exceeds %d bytes", shadowLogMax)
	}
	log, err := openNudgeLog(store.dir)
	if err != nil {
		return "", err
	}
	threads := store.Threads[:min(len(store.Threads), shadowThreadsMax)]
	goEligible := make(map[string]bool, len(threads))
	for _, nudge := range collectNudges(&Store{Threads: threads}, log, now, nil) {
		goEligible[nudge.EntityID] = true
	}
	report := struct {
		Status       string   `json:"status"`
		Checked      int      `json:"threads_checked"`
		Skipped      int      `json:"threads_skipped"`
		Mismatches   int      `json:"mismatches"`
		GoEligible   int      `json:"go_eligible"`
		BendEligible int      `json:"bend_eligible"`
		CasesSeen    []int    `json:"cases_seen"`
		MismatchIDs  []string `json:"mismatch_ids,omitempty"`
		DurationUS   int64    `json:"duration_us"`
		Policy       string   `json:"policy"`
	}{Status: "ok", Checked: len(threads), Skipped: len(store.Threads) - len(threads), Policy: "bend-2.0.5/eligibility-v1"}
	var casesSeen [shadowTableSize]bool
	if report.Skipped != 0 {
		report.Status = "partial"
	}
	for _, thread := range threads {
		caseIndex, err := bendCaseIndex(thread, log, now)
		if err != nil {
			return "", err
		}
		casesSeen[caseIndex] = true
		bendEligible := table[caseIndex] == '1'
		if goEligible[thread.ID] {
			report.GoEligible++
		}
		if bendEligible {
			report.BendEligible++
		}
		if bendEligible != goEligible[thread.ID] {
			report.Status = "mismatch"
			report.Mismatches++
			if len(report.MismatchIDs) < 8 {
				report.MismatchIDs = append(report.MismatchIDs, thread.ShortID())
			}
		}
	}
	for index, seen := range casesSeen {
		if seen {
			report.CasesSeen = append(report.CasesSeen, index)
		}
	}
	report.DurationUS = time.Since(started).Microseconds()
	data, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("shadow: encode report: %w", err)
	}
	return string(data), nil
}

func bendCaseIndex(thread *Thread, log *nudgeLog, now time.Time) (int, error) {
	state := 0
	recent, overdue, fires := false, false, 0
	switch thread.State {
	case ThreadNew:
	case ThreadNeedsReply:
		state = 1
		recent = log.firesWithin("needs_reply", thread.ID, replyCooldown, now) != 0
		overdue = now.Sub(thread.LastInbound) > replyOverdue
		fires = min(2, log.firesWithin("needs_reply", thread.ID, nudgeLogMaxAge, now))
	case ThreadWaitingOnThem:
		state = 2
		recent = log.firesWithin("bump", thread.ID, bumpQuiet, now) != 0
		overdue = now.Sub(thread.LastOutbound) > bumpQuiet
	case ThreadFYI:
		state = 3
	case ThreadNoise:
		state = 4
	case ThreadDone:
		state = 5
	default:
		return 0, fmt.Errorf("shadow: unknown thread state %q", thread.State)
	}
	flags := 0
	if thread.Snoozed(now) {
		flags |= 1
	}
	if recent {
		flags |= 2
	}
	if overdue {
		flags |= 4
	}
	return state*24 + flags*3 + fires, nil
}

func productionBendEligibility(store *Store, cfg *Config, nl *nudgeLog, now time.Time) map[*Thread]bool {
	info, err := os.Stat(filepath.Join(store.dir, "bend-eligibility.enabled"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		log.Printf("nudge: invalid Bend switch, using Go eligibility")
		return nil
	}
	if len(store.Threads) > shadowThreadsMax {
		log.Printf("nudge: Bend thread limit exceeded, using Go eligibility")
		return nil
	}
	table, err := readBendTable(cfg.BendShadow, eligibilityProtocol)
	if err != nil {
		log.Printf("nudge: using Go eligibility: %v", err)
		return nil
	}
	eligible := make(map[*Thread]bool, len(store.Threads))
	for _, thread := range store.Threads {
		index, err := bendCaseIndex(thread, nl, now)
		if err != nil {
			log.Printf("nudge: using Go eligibility: %v", err)
			return nil
		}
		eligible[thread] = table[index] == '1'
	}
	log.Printf("nudge: Bend eligibility active for %d threads", len(eligible))
	return eligible
}
