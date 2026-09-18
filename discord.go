package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	discordAPI        = "https://discord.com/api/v10"
	discordTimeout    = 30 * time.Second
	discordChunkChars = 1900
	discordMaxChunks  = 20
	discordFence      = "```\n\n```"
	discordMaxBody    = 1 << 20
)

type DiscordMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Author  struct {
		ID       string `json:"id"`
		Bot      bool   `json:"bot"`
		Username string `json:"username"`
	} `json:"author"`
	// Present when the message is a reply; Discord inlines the whole message.
	Referenced *DiscordMessage `json:"referenced_message"`
}

func discordDo(cfg *Config, method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("discord: marshal: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, discordAPI+path, payload)
	if err != nil {
		return nil, fmt.Errorf("discord: request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+cfg.DiscordToken)
	req.Header.Set("User-Agent", "DiscordBot (https://ethanthoma.com/tether, 0.1)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: discordTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, discordMaxBody))
	if err != nil {
		return nil, fmt.Errorf("discord: read: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord: %s: status %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func PostDiscord(cfg *Config, text string) error {
	_, err := PostDiscordMessage(cfg, text)
	return err
}

// Returns the id of the first message posted, which is what a thread hangs off.
func PostDiscordMessage(cfg *Config, text string) (string, error) {
	first := ""
	for _, chunk := range chunkMessage(text, discordChunkChars) {
		data, err := discordDo(cfg, "POST", "/channels/"+cfg.DiscordChannel+"/messages",
			map[string]string{"content": chunk})
		if err != nil {
			return "", err
		}
		if first == "" {
			var posted struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &posted); err != nil {
				return "", fmt.Errorf("discord: decode posted message: %w", err)
			}
			first = posted.ID
		}
	}
	return first, nil
}

// Buttons ride on the last chunk so they sit directly under the text they act on.
func PostDiscordButtons(cfg *Config, text string, buttons []map[string]any) error {
	chunks := chunkMessage(text, discordChunkChars)
	if len(chunks) == 0 {
		return nil
	}
	if err := postChunks(cfg, chunks[:len(chunks)-1]); err != nil {
		return err
	}
	payload := map[string]any{"content": chunks[len(chunks)-1]}
	if len(buttons) > 0 {
		payload["components"] = []map[string]any{{"type": 1, "components": buttons}}
	}
	_, err := discordDo(cfg, "POST", "/channels/"+cfg.DiscordChannel+"/messages", payload)
	return err
}

func Button(label, customID string) map[string]any {
	return map[string]any{"type": 2, "style": 2, "label": label, "custom_id": customID}
}

// Each chunk carries its own fence: Discord closes code blocks at the message
// boundary, so one fence wrapped around a split reply renders as broken markup.
func fenceChunks(text string) []string {
	text = strings.ReplaceAll(text, "```", "'''")
	var fenced []string
	for _, chunk := range chunkMessage(text, discordChunkChars-len(discordFence)) {
		fenced = append(fenced, "```\n"+chunk+"\n```")
	}
	return fenced
}

func postChunks(cfg *Config, chunks []string) error {
	for _, chunk := range chunks {
		path := "/channels/" + cfg.DiscordChannel + "/messages"
		if _, err := discordDo(cfg, "POST", path, map[string]string{"content": chunk}); err != nil {
			return err
		}
	}
	return nil
}

func FetchDiscord(cfg *Config, after string, limit int) ([]DiscordMessage, error) {
	path := "/channels/" + cfg.DiscordChannel + "/messages?limit=" + strconv.Itoa(limit)
	if after != "" {
		path += "&after=" + after
	}
	data, err := discordDo(cfg, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var messages []DiscordMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("discord: decode messages: %w", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func chunkMessage(text string, width int) []string {
	text = strings.TrimSpace(text)
	var chunks []string
	for text != "" && len(chunks) < discordMaxChunks {
		chunk := text
		if len(chunk) > width {
			cut := strings.LastIndex(chunk[:width], "\n")
			if cut < width/2 {
				cut = width
			}
			chunk = chunk[:cut]
		}
		text = strings.TrimPrefix(text[len(chunk):], "\n")
		chunks = append(chunks, chunk)
	}
	if text != "" && len(chunks) > 0 {
		chunks[len(chunks)-1] += "\n… truncated"
	}
	return chunks
}
