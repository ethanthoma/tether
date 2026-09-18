package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func goBatchCount(quiet bool, fired, candidates int) int {
	if quiet {
		return 0
	}
	return min(max(0, maxPushesPerDay-fired), candidates)
}

func bendBatchCount(table []byte, quiet bool, fired, candidates int) int {
	index := min(fired, 5)*6 + min(candidates, 5)
	if quiet {
		index += 36
	}
	return int(table[index] - '0')
}

func productionBendBatchCount(store *Store, cfg *Config, quiet bool, fired, candidates int) int {
	fallback := goBatchCount(quiet, fired, candidates)
	info, err := os.Stat(filepath.Join(store.dir, "bend-dispatch.enabled"))
	if os.IsNotExist(err) {
		return fallback
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		log.Printf("nudge: invalid Bend dispatch switch, using Go batch size")
		return fallback
	}
	table, err := readBendTable(cfg.BendDispatch, dispatchProtocol)
	if err != nil {
		log.Printf("nudge: using Go batch size: %v", err)
		return fallback
	}
	planned := bendBatchCount(table, quiet, fired, candidates)
	if planned > fallback {
		log.Printf("nudge: Bend dispatch exceeds Go safety limit, using Go batch size")
		return fallback
	}
	log.Printf("nudge: Bend dispatch active, selected %d of %d candidates", planned, candidates)
	return planned
}

func RunBendDispatchShadow(store *Store, evaluator string, now time.Time) (string, error) {
	started := time.Now()
	if len(store.Threads) > shadowThreadsMax {
		return "", fmt.Errorf("dispatch shadow: thread limit exceeded")
	}
	table, err := readBendTable(evaluator, dispatchProtocol)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(filepath.Join(store.dir, "nudges.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if info != nil && info.Size() > shadowLogMax {
		return "", fmt.Errorf("dispatch shadow: nudge log exceeds limit")
	}
	nl, err := openNudgeLog(store.dir)
	if err != nil {
		return "", err
	}
	snapshot := *store
	snapshot.Commitments = make([]*Commitment, len(store.Commitments))
	for index, commitment := range store.Commitments {
		copy := *commitment
		copy.Slip(now)
		snapshot.Commitments[index] = &copy
	}
	candidates := len(collectNudges(&snapshot, nl, now, nil))
	quiet, fired := inQuietHours(now), nl.firedToday("", now)
	goCount := goBatchCount(quiet, fired, candidates)
	bendCount := bendBatchCount(table, quiet, fired, candidates)
	report := struct {
		Status     string `json:"status"`
		Policy     string `json:"policy"`
		Quiet      bool   `json:"quiet"`
		Fired      int    `json:"fired_today"`
		Candidates int    `json:"candidates"`
		GoCount    int    `json:"go_batch"`
		BendCount  int    `json:"bend_batch"`
		DurationUS int64  `json:"duration_us"`
	}{"ok", "bend-2.0.5/dispatch-v1", quiet, fired, candidates, goCount, bendCount, time.Since(started).Microseconds()}
	if goCount != bendCount {
		report.Status = "mismatch"
	}
	data, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
