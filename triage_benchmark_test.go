package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type triageExample struct {
	ID         string `json:"id"`
	Expected   string `json:"expected"`
	Rationale  string `json:"rationale"`
	Provenance string `json:"provenance"`
	Messages   []struct {
		Outbound bool   `json:"outbound"`
		Body     string `json:"body"`
	} `json:"messages"`
}

type triageScores struct {
	Cases               int                       `json:"cases"`
	Accepted            int                       `json:"accepted"`
	CorrectAccepted     int                       `json:"correct_accepted"`
	Abstained           int                       `json:"abstained"`
	CorrectAbstentions  int                       `json:"correct_abstentions"`
	FalseReminders      int                       `json:"false_reminders"`
	FalseSilencing      int                       `json:"false_silencing"`
	ActionableAbstained int                       `json:"actionable_abstained"`
	AmbiguousAccepted   int                       `json:"ambiguous_accepted"`
	Confusion           map[string]map[string]int `json:"confusion"`
}

func triageLabelValid(label string) bool {
	switch label {
	case "needs_reply", "waiting_on_them", "fyi", "noise", "abstain":
		return true
	}
	return false
}

func loadTriageExamples(t *testing.T) []triageExample {
	t.Helper()
	data, err := os.ReadFile("experiments/triage/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var dataset struct {
		Version      int             `json:"version"`
		ReviewStatus string          `json:"review_status"`
		Cases        []triageExample `json:"cases"`
	}
	if err := json.Unmarshal(data, &dataset); err != nil {
		t.Fatal(err)
	}
	if dataset.Version != 1 || dataset.ReviewStatus != "provisional_assistant_labels" || len(dataset.Cases) == 0 || len(dataset.Cases) > 256 {
		t.Fatal("invalid dataset metadata")
	}
	seen := map[string]bool{}
	for _, example := range dataset.Cases {
		if example.ID == "" || seen[example.ID] || !triageLabelValid(example.Expected) || example.Rationale == "" {
			t.Fatal("invalid or duplicate case")
		}
		if example.Provenance != "synthetic" && example.Provenance != "paraphrased_history" {
			t.Fatal("missing provenance")
		}
		if len(example.Messages) == 0 || len(example.Messages) > 2 {
			t.Fatal("invalid message count")
		}
		for _, message := range example.Messages {
			if len(message.Body) == 0 || len(message.Body) > 4000 {
				t.Fatal("invalid message size")
			}
		}
		seen[example.ID] = true
	}
	return dataset.Cases
}

func triageLexicalBaseline(example triageExample) string {
	latest := example.Messages[len(example.Messages)-1]
	body := strings.ToLower(latest.Body)
	body, _, _ = strings.Cut(body, "\n\non ")
	body, _, _ = strings.Cut(body, "\n>")
	if strings.Contains(body, "unsubscribe") && (strings.Contains(body, "newsletter") || strings.Contains(body, "limited time offer")) {
		return "noise"
	}
	if strings.Contains(body, "calendar notifications") || strings.Contains(body, "accept or ignore") || strings.Contains(body, "no reply needed") {
		return "fyi"
	}
	if strings.Contains(body, "could you") || strings.Contains(body, "please confirm") || strings.Contains(body, "please send") {
		if latest.Outbound && strings.Contains(body, "i will") {
			return "abstain"
		}
		if latest.Outbound {
			return "waiting_on_them"
		}
		return "needs_reply"
	}
	if strings.HasPrefix(body, "thank you") || strings.Contains(body, "that resolves everything") {
		return "fyi"
	}
	return "abstain"
}

func scoreTriage(examples []triageExample, predictions map[string]string) (triageScores, error) {
	scores := triageScores{Cases: len(examples), Confusion: map[string]map[string]int{}}
	if len(predictions) != len(examples) {
		return scores, fmt.Errorf("need exactly one prediction per case")
	}
	for _, example := range examples {
		predicted, exists := predictions[example.ID]
		if !exists || !triageLabelValid(predicted) {
			return scores, fmt.Errorf("missing or invalid prediction for %s", example.ID)
		}
		if scores.Confusion[example.Expected] == nil {
			scores.Confusion[example.Expected] = map[string]int{}
		}
		scores.Confusion[example.Expected][predicted]++
		actionable := example.Expected == "needs_reply" || example.Expected == "waiting_on_them"
		if predicted == "abstain" {
			scores.Abstained++
			if example.Expected == "abstain" {
				scores.CorrectAbstentions++
			}
			if actionable {
				scores.ActionableAbstained++
			}
			continue
		}
		scores.Accepted++
		if predicted == example.Expected {
			scores.CorrectAccepted++
		}
		if example.Expected == "abstain" {
			scores.AmbiguousAccepted++
		}
		if (predicted == "needs_reply" || predicted == "waiting_on_them") && predicted != example.Expected {
			scores.FalseReminders++
		}
		if actionable && (predicted == "fyi" || predicted == "noise") {
			scores.FalseSilencing++
		}
	}
	return scores, nil
}

func reportTriage(t *testing.T, examples []triageExample, predictions map[string]string) {
	t.Helper()
	scores, err := scoreTriage(examples, predictions)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(scores)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
	for _, example := range examples {
		if predictions[example.ID] != example.Expected {
			t.Logf("%s: predicted=%s expected=%s", example.ID, predictions[example.ID], example.Expected)
		}
	}
}

func TestTriageOfflineBaseline(t *testing.T) {
	examples := loadTriageExamples(t)
	predictions := map[string]string{}
	for _, example := range examples {
		predictions[example.ID] = triageLexicalBaseline(example)
	}
	reportTriage(t, examples, predictions)
}

func TestTriageImportedPredictions(t *testing.T) {
	path := os.Getenv("TETHER_TRIAGE_PREDICTIONS")
	if path == "" {
		t.Skip("set TETHER_TRIAGE_PREDICTIONS to a JSON object mapping case IDs to labels")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		t.Fatal("prediction file must be regular and at most 1 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var predictions map[string]string
	if err := json.Unmarshal(data, &predictions); err != nil {
		t.Fatal(err)
	}
	reportTriage(t, loadTriageExamples(t), predictions)
}

func TestTriageScoringErrorsAndAbstention(t *testing.T) {
	examples := []triageExample{{ID: "a", Expected: "needs_reply"}, {ID: "b", Expected: "fyi"}, {ID: "c", Expected: "abstain"}, {ID: "d", Expected: "waiting_on_them"}}
	scores, err := scoreTriage(examples, map[string]string{"a": "fyi", "b": "needs_reply", "c": "noise", "d": "abstain"})
	if err != nil {
		t.Fatal(err)
	}
	if scores.Accepted != 3 || scores.FalseReminders != 1 || scores.FalseSilencing != 1 || scores.ActionableAbstained != 1 || scores.AmbiguousAccepted != 1 {
		t.Fatalf("incorrect scores: %+v", scores)
	}
	for _, predictions := range []map[string]string{{}, {"a": "done", "b": "fyi", "c": "abstain", "d": "abstain"}, {"x": "fyi", "b": "fyi", "c": "abstain", "d": "abstain"}} {
		if _, err := scoreTriage(examples, predictions); err == nil {
			t.Fatal("accepted invalid predictions")
		}
	}
}
