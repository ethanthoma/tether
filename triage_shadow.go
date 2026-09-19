package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const triageShadowTimeout = 30 * time.Second

type triageShadowMessage struct {
	Outbound bool   `json:"outbound"`
	Body     string `json:"body"`
}

type triageShadowCase struct {
	ID       string                `json:"id"`
	Messages []triageShadowMessage `json:"messages"`
}

type triageShadowResult struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Confidence *float64 `json:"confidence"`
	Status     string   `json:"status"`
}

type triageShadowResponse struct {
	Version     int                  `json:"version"`
	Policy      string               `json:"policy"`
	ModelSHA256 string               `json:"model_sha256"`
	Results     []triageShadowResult `json:"results"`
}

type triageShadowPrediction struct {
	ThreadHash  string      `json:"thread_hash"`
	StoredState ThreadState `json:"stored_state"`
	Label       string      `json:"label"`
	Confidence  float64     `json:"confidence"`
	Status      string      `json:"status"`
}

func RunTriageShadow(store *Store, evaluator string, now time.Time) (string, error) {
	started := time.Now()
	if !filepath.IsAbs(evaluator) {
		return "", fmt.Errorf("triage shadow: absolute evaluator path required")
	}
	candidates := make([]*Thread, 0, min(len(store.Threads), shadowThreadsMax))
	for _, thread := range store.Threads[:min(len(store.Threads), shadowThreadsMax)] {
		if thread == nil || !thread.State.valid() || thread.State == ThreadDone {
			continue
		}
		candidates = append(candidates, thread)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i].LastInbound, candidates[j].LastInbound
		if candidates[i].LastOutbound.After(left) {
			left = candidates[i].LastOutbound
		}
		if candidates[j].LastOutbound.After(right) {
			right = candidates[j].LastOutbound
		}
		if left.Equal(right) {
			return candidates[i].ID < candidates[j].ID
		}
		return left.After(right)
	})
	cases := make([]triageShadowCase, 0, triageMaxPerRun)
	threads := make(map[string]*Thread, triageMaxPerRun)
	for _, thread := range candidates[:min(len(candidates), triageMaxPerRun)] {
		messages, err := triageShadowMessages(store, thread)
		if err != nil {
			continue
		}
		identifier := strconv.Itoa(len(cases))
		cases = append(cases, triageShadowCase{ID: identifier, Messages: messages})
		threads[identifier] = thread
	}
	report := struct {
		Mode          string                   `json:"mode"`
		ObservedAt    time.Time                `json:"observed_at"`
		ModelPolicy   string                   `json:"model_policy"`
		PolicyAligned bool                     `json:"policy_aligned"`
		ModelSHA256   string                   `json:"model_sha256,omitempty"`
		Checked       int                      `json:"checked"`
		Skipped       int                      `json:"skipped"`
		Abstained     int                      `json:"abstained"`
		InputRejected int                      `json:"input_rejected"`
		Comparisons   int                      `json:"comparisons"`
		Disagreements int                      `json:"disagreements"`
		DurationMS    int64                    `json:"duration_ms"`
		Predictions   []triageShadowPrediction `json:"predictions"`
	}{Mode: "read_only", ObservedAt: now.UTC(), ModelPolicy: "synthetic-obligations-v1", Skipped: len(store.Threads) - len(cases)}
	if len(cases) != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), triageShadowTimeout)
		defer cancel()
		response, err := readTriageShadow(ctx, evaluator, cases)
		if err != nil {
			return "", err
		}
		report.ModelSHA256 = response.ModelSHA256
		for _, result := range response.Results {
			thread := threads[result.ID]
			sum := sha256.Sum256([]byte(thread.ID))
			report.Predictions = append(report.Predictions, triageShadowPrediction{hex.EncodeToString(sum[:]), thread.State, result.Label, *result.Confidence, result.Status})
			report.Checked++
			if result.Label == "abstain" {
				report.Abstained++
			}
			if result.Status == "input_rejected" {
				report.InputRejected++
			}
			if result.Status == "ok" && result.Label != "abstain" && thread.State != ThreadNew {
				report.Comparisons++
				if result.Label != string(thread.State) {
					report.Disagreements++
				}
			}
		}
	}
	report.DurationMS = time.Since(started).Milliseconds()
	data, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("triage shadow: report encoding failed")
	}
	return string(data), nil
}

func triageShadowMessages(store *Store, thread *Thread) ([]triageShadowMessage, error) {
	if len(thread.MsgIDs) == 0 {
		return nil, fmt.Errorf("triage shadow: no cached messages")
	}
	identifiers := thread.MsgIDs[max(0, len(thread.MsgIDs)-triageMsgsInPrompt):]
	messages := make([]triageShadowMessage, 0, len(identifiers))
	for _, identifier := range identifiers {
		file, err := os.Open(store.msgPath(identifier))
		if err != nil {
			return nil, fmt.Errorf("triage shadow: message unavailable")
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			file.Close()
			return nil, fmt.Errorf("triage shadow: message file rejected")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 64<<10 {
			return nil, fmt.Errorf("triage shadow: message read failed")
		}
		var message Message
		if !utf8.Valid(data) || json.Unmarshal(data, &message) != nil || message.MsgID != identifier || len(message.Body) > triageMsgChars || strings.TrimSpace(message.Body) == "" {
			return nil, fmt.Errorf("triage shadow: message content rejected")
		}
		messages = append(messages, triageShadowMessage{Outbound: message.Outbound, Body: message.Body})
	}
	return messages, nil
}

func readTriageShadow(ctx context.Context, evaluator string, cases []triageShadowCase) (triageShadowResponse, error) {
	var response triageShadowResponse
	if !filepath.IsAbs(evaluator) || len(cases) == 0 || len(cases) > triageMaxPerRun {
		return response, fmt.Errorf("triage shadow: invalid evaluator or batch")
	}
	expected := make(map[string]bool, len(cases))
	for _, item := range cases {
		if item.ID == "" || expected[item.ID] {
			return response, fmt.Errorf("triage shadow: invalid request IDs")
		}
		expected[item.ID] = true
	}
	request, err := json.Marshal(struct {
		Version int                `json:"version"`
		Cases   []triageShadowCase `json:"cases"`
	}{1, cases})
	if err != nil || len(request) > 256<<10 {
		return response, fmt.Errorf("triage shadow: invalid request size")
	}
	command := exec.CommandContext(ctx, evaluator)
	command.Env = []string{"HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1", "TOKENIZERS_PARALLELISM=false", "OMP_NUM_THREADS=4", "OPENBLAS_NUM_THREADS=4", "PYTHONNOUSERSITE=1", "LANG=C.UTF-8"}
	if libraryPath := os.Getenv("LD_LIBRARY_PATH"); libraryPath != "" {
		command.Env = append(command.Env, "LD_LIBRARY_PATH="+libraryPath)
	}
	command.Stdin = bytes.NewReader(request)
	command.Stderr = io.Discard
	command.WaitDelay = 100 * time.Millisecond
	output := shadowOutput{limit: 32 << 10}
	command.Stdout = &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return response, fmt.Errorf("triage shadow: evaluator timed out or canceled")
		}
		return response, fmt.Errorf("triage shadow: evaluator failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(output.buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil {
		return response, fmt.Errorf("triage shadow: invalid response")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return response, fmt.Errorf("triage shadow: trailing output")
	}
	hash, err := hex.DecodeString(response.ModelSHA256)
	if response.Version != 1 || response.Policy != "synthetic-obligations-v1" || err != nil || len(hash) != sha256.Size || strings.ToLower(response.ModelSHA256) != response.ModelSHA256 || len(response.Results) != len(cases) {
		return response, fmt.Errorf("triage shadow: invalid response metadata")
	}
	for _, result := range response.Results {
		if !expected[result.ID] {
			return response, fmt.Errorf("triage shadow: unexpected or duplicate result ID")
		}
		delete(expected, result.ID)
		switch result.Label {
		case "needs_reply", "waiting_on_them", "fyi", "noise", "abstain":
		default:
			return response, fmt.Errorf("triage shadow: invalid label")
		}
		if result.Confidence == nil || math.IsNaN(*result.Confidence) || math.IsInf(*result.Confidence, 0) || *result.Confidence < 0 || *result.Confidence > 1 {
			return response, fmt.Errorf("triage shadow: invalid confidence")
		}
		if result.Status != "ok" && result.Status != "input_rejected" {
			return response, fmt.Errorf("triage shadow: invalid result status")
		}
		if result.Status == "input_rejected" && (result.Label != "abstain" || *result.Confidence != 0) {
			return response, fmt.Errorf("triage shadow: invalid rejected result")
		}
	}
	return response, nil
}
