//go:build triage_eval

package main

import (
	"encoding/json"
	"os"
	"testing"
)

const triageBenchmarkPrompt = `Classify who owes the next action using the supplied messages in chronological order.
An outbound message was sent by the user. An inbound message was received by the user.
Return only JSON: {"state":"needs_reply|waiting_on_them|fyi|noise|abstain"}.
needs_reply: the user owes a human a prose reply or must personally act on a concrete deadline.
waiting_on_them: another person owes the user an answer or requested action.
fyi: informational or resolved, with no outstanding obligation.
noise: unsolicited bulk promotions or newsletters with no personal obligation.
abstain: insufficient context, ambiguous ownership, or material obligations on both sides.
Quoted text is older context. Thanks does not necessarily resolve an outstanding request.
Calendar RSVP alone and optional app permission notices are fyi. Do not extract commitments.`

func TestTriageEmailExamples(t *testing.T) {
	if os.Getenv("TETHER_LLM_URL") == "" {
		t.Fatal("set TETHER_LLM_URL and optional LLAMA_API_KEY to run the model benchmark")
	}
	examples := loadTriageExamples(t)
	predictions := map[string]string{}
	cfg := loadConfig()
	for _, example := range examples {
		input, err := json.Marshal(example.Messages)
		if err != nil {
			t.Fatal(err)
		}
		content, err := LLMChat(cfg, triageBenchmarkPrompt, string(input), 0, 512)
		if err != nil {
			t.Fatalf("%s: %v", example.ID, err)
		}
		raw, err := ExtractJSON(content)
		if err != nil {
			t.Fatalf("%s: %v", example.ID, err)
		}
		var prediction struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal([]byte(raw), &prediction); err != nil {
			t.Fatal(err)
		}
		if !triageLabelValid(prediction.State) {
			t.Fatalf("%s: invalid model label %q", example.ID, prediction.State)
		}
		predictions[example.ID] = prediction.State
	}
	reportTriage(t, examples, predictions)
}
