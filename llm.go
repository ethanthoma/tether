package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const llmTimeout = 5 * time.Minute

var errLLMUnavailable = errors.New("llm service unavailable")

type llmRequest struct {
	Messages    []llmMessage     `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	Tools       []map[string]any `json:"tools,omitempty"`
}

type ToolCall struct {
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
			// Reasoning models return their chain of thought here and leave content
			// empty when max_tokens cuts them off mid-thought.
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func LLMChat(cfg *Config, system, user string, temperature float64, maxTokens int) (string, error) {
	content, _, err := llmComplete(cfg, system, user, nil, temperature, maxTokens)
	return content, err
}

// With tools the model may answer with calls and no prose, so an empty content
// is only an error when nothing at all came back.
func LLMWithTools(cfg *Config, system, user string, tools []map[string]any,
	temperature float64, maxTokens int) (string, []ToolCall, error) {
	return llmComplete(cfg, system, user, tools, temperature, maxTokens)
}

func llmComplete(cfg *Config, system, user string, tools []map[string]any,
	temperature float64, maxTokens int) (string, []ToolCall, error) {
	body, err := json.Marshal(llmRequest{
		Messages:    []llmMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Tools:       tools,
	})
	if err != nil {
		return "", nil, fmt.Errorf("%w: marshal: %w", errLLMUnavailable, err)
	}
	req, err := http.NewRequest("POST", cfg.LLMURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("%w: request: %w", errLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.LLMKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.LLMKey)
	}
	client := &http.Client{Timeout: llmTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%w: post: %w", errLLMUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("%w: HTTP status %d", errLLMUnavailable, resp.StatusCode)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", nil, fmt.Errorf("%w: read: %w", errLLMUnavailable, err)
	}
	var parsed llmResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", nil, fmt.Errorf("%w: parse response (status %s): %w", errLLMUnavailable, resp.Status, err)
	}
	if parsed.Error != nil {
		return "", nil, fmt.Errorf("%w: server error: %s", errLLMUnavailable, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", nil, fmt.Errorf("%w: empty response (status %s)", errLLMUnavailable, resp.Status)
	}
	choice := parsed.Choices[0]
	if len(choice.Message.ToolCalls) > 0 {
		return choice.Message.Content, choice.Message.ToolCalls, nil
	}
	// Never fall back to reasoning_content: it is chain of thought, and callers
	// render this straight to the user. An empty answer is the honest result.
	if strings.TrimSpace(choice.Message.Content) == "" {
		if choice.Message.ReasoningContent != "" {
			return "", nil, fmt.Errorf("llm: model spent all %d tokens reasoning and answered nothing (finish_reason %q)",
				maxTokens, choice.FinishReason)
		}
		return "", nil, fmt.Errorf("llm: empty content (finish_reason %q)", choice.FinishReason)
	}
	return choice.Message.Content, nil, nil
}

func ExtractJSON(content string) (string, error) {
	start := bytes.IndexByte([]byte(content), '{')
	if start < 0 {
		return "", fmt.Errorf("llm: no JSON object in output")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		c := content[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return content[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("llm: unterminated JSON object in output")
}
