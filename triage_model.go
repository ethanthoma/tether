package main

import (
	"context"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func productionTriageHash(store *Store) string {
	path := filepath.Join(store.dir, "triage-model.enabled")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() < 64 || info.Size() > 65 {
		log.Printf("triage: invalid model switch, using existing triage")
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		log.Printf("triage: model switch unavailable")
		return ""
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 66))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 65 {
		log.Printf("triage: model switch unreadable")
		return ""
	}
	approved := strings.TrimSpace(string(data))
	_, hashErr := hex.DecodeString(approved)
	if len(approved) != 64 || hashErr != nil || approved != strings.ToLower(approved) {
		log.Printf("triage: invalid model hash")
		return ""
	}
	return approved
}

func productionTriagePredictions(store *Store, evaluator, approved string, candidates []*Thread) map[*Thread]ThreadState {
	if approved == "" {
		return nil
	}
	cases := make([]triageShadowCase, 0, triageMaxPerRun)
	threads := make(map[string]*Thread, triageMaxPerRun)
	for _, thread := range candidates[:min(len(candidates), triageMaxPerRun)] {
		messages, err := triageShadowMessages(store, thread)
		if err != nil {
			continue
		}
		identifier := strconv.Itoa(len(cases))
		threads[identifier] = thread
		cases = append(cases, triageShadowCase{ID: identifier, Messages: messages})
	}
	if len(cases) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), triageShadowTimeout)
	defer cancel()
	response, err := readTriageShadow(ctx, evaluator, cases)
	if err != nil {
		log.Printf("triage: local model unavailable: %v", err)
		return nil
	}
	if response.Policy != "reply-triage-v2" || response.ModelSHA256 != approved {
		log.Printf("triage: unapproved model identity or policy, using existing triage")
		return nil
	}
	classified := make(map[*Thread]ThreadState, len(cases))
	for _, result := range response.Results {
		if result.Status != "ok" || result.Label == "abstain" {
			continue
		}
		classified[threads[result.ID]] = ThreadState(result.Label)
	}
	log.Printf("triage: v2 model active, checked=%d accepted=%d abstained_or_rejected=%d", len(cases), len(classified), len(cases)-len(classified))
	return classified
}
