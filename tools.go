package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const toolMaxCalls = 3

// Tool schemas are generated from the same command specs the slash commands and
// buttons use, so a command can never mean two different things.
func toolSchemas() []map[string]any {
	var tools []map[string]any
	for _, command := range slashCommands {
		if !command.Tool {
			continue
		}
		properties := map[string]any{}
		var required []string
		for _, option := range command.Options {
			property := map[string]any{"type": "string", "description": option.Description}
			if option.Type == optionInteger {
				property["type"] = "integer"
			}
			if len(option.Choices) > 0 {
				property["enum"] = option.Choices
			}
			properties[option.Name] = property
			if option.Required {
				required = append(required, option.Name)
			}
		}
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        command.Name,
				"description": command.Description,
				"parameters": map[string]any{
					"type": "object", "properties": properties, "required": required,
				},
			},
		})
	}
	return tools
}

// Runs what the model asked for and reports each result. Bounded, and every call
// goes through Execute, so tools inherit the same validation as typed commands.
func runToolCalls(store *Store, cfg *Config, now time.Time, calls []ToolCall) string {
	var lines []string
	for i, call := range calls {
		if i >= toolMaxCalls {
			lines = append(lines, fmt.Sprintf("(stopped after %d actions)", toolMaxCalls))
			break
		}
		command := call.Function.Name
		spec := false
		for _, c := range slashCommands {
			if c.Name == command && c.Tool {
				spec = true
			}
		}
		if !spec {
			lines = append(lines, fmt.Sprintf("refused %q: not an allowed action", command))
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &raw); err != nil {
			lines = append(lines, fmt.Sprintf("%s: bad arguments (%v)", command, err))
			continue
		}
		values := make(map[string]string, len(raw))
		for name, value := range raw {
			values[name] = fmt.Sprintf("%v", value)
		}
		log.Printf("chat: tool %s %v", command, values)
		out, err := Execute(store, cfg, now, command, orderedArgs(command, values))
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s failed: %v", command, err))
			continue
		}
		lines = append(lines, strings.TrimSpace(out))
	}
	return strings.Join(lines, "\n")
}
