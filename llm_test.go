package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"state":"fyi"}`, `{"state":"fyi"}`},
		{"the answer is:\n{\"a\": {\"b\": 1}}\ntrailing", `{"a": {"b": 1}}`},
		{`{"note": "has } brace and \" quote in string"}`, `{"note": "has } brace and \" quote in string"}`},
	}
	for _, c := range cases {
		got, err := ExtractJSON(c.in)
		if err != nil || got != c.want {
			t.Fatalf("ExtractJSON(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"no json here", `{"unterminated": true`} {
		if _, err := ExtractJSON(bad); err == nil {
			t.Fatalf("ExtractJSON(%q) should fail", bad)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	v, err := parseVerdict(`reasoning... {"state": "needs_reply", "note": "bob asked a question", "commitments": []}`)
	if err != nil || v.State != ThreadNeedsReply {
		t.Fatalf("verdict: %+v, %v", v, err)
	}
	if _, err := parseVerdict(`{"state": "done", "note": "x"}`); err == nil {
		t.Fatal("done is not a valid triage verdict")
	}
}

func TestLLMChatErrorsWhenNothingReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":""}}]}`)
	}))
	defer server.Close()
	if _, err := LLMChat(&Config{LLMURL: server.URL}, "sys", "user", 0, 100); err == nil {
		t.Fatal("want an error naming finish_reason, got nil")
	}
}

func TestLLMChatRefusesReasoningOnlyOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"Self-Correction during thought: the prompt says..."}}]}`)
	}))
	defer server.Close()
	got, err := LLMChat(&Config{LLMURL: server.URL}, "sys", "user", 0, 100)
	if err == nil {
		t.Fatalf("want an error, got %q", got)
	}
	if got != "" {
		t.Errorf("reasoning leaked to the caller: %q", got)
	}
	if !strings.Contains(err.Error(), "reasoning") {
		t.Errorf("error should name the cause, got: %v", err)
	}
}
