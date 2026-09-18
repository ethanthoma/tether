package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

const (
	chatPollInterval    = 5 * time.Second
	chatErrorBackoff    = 60 * time.Second
	chatFetchLimit      = 25
	chatMaxTurns        = 3
	chatConversationTTL = time.Hour
	chatThreadTTL       = 24 * time.Hour
	chatAnswerMaxChars  = 1500
)

type Turn struct {
	Question string `json:"q"`
	Answer   string `json:"a"`
}

type Conversation struct {
	Turns   []Turn    `json:"turns"`
	Updated time.Time `json:"updated"`
}

// Written only by the bot process, so it needs no store lock. The thread tether
// is listening in lives in the store instead, because the digest process sets it.
type ChatState struct {
	Cursors       map[string]string        `json:"cursors"`
	Conversations map[string]*Conversation `json:"conversations"`
}

func (s *ChatState) init() {
	if s.Cursors == nil {
		s.Cursors = map[string]string{}
	}
	if s.Conversations == nil {
		s.Conversations = map[string]*Conversation{}
	}
}

// Expiry keeps both maps bounded without a separate sweep.
func (s *ChatState) prune(now time.Time, live map[string]bool) {
	for id := range s.Cursors {
		if !live[id] {
			delete(s.Cursors, id)
		}
	}
	for id, c := range s.Conversations {
		if now.Sub(c.Updated) > chatConversationTTL || !live[id] {
			delete(s.Conversations, id)
		}
	}
}

// Read without the store lock: saveJSON renames into place, so a reader either
// sees the whole old file or the whole new one, never a torn one.
func listeningThread(cfg *Config, now time.Time) string {
	var sync SyncState
	if err := loadJSON(filepath.Join(cfg.StateDir, "sync.json"), &sync); err != nil {
		return ""
	}
	if sync.ChatThread == "" || now.Sub(sync.ChatThreadAt) > chatThreadTTL {
		return ""
	}
	return sync.ChatThread
}

func chatStatePath(cfg *Config) string { return filepath.Join(cfg.StateDir, "chat.json") }

// Only replies to tether's own messages and messages inside threads tether
// opened are treated as input. Everything else in the channel is ignored.
func RunChat(cfg *Config, appID string) {
	var state ChatState
	if err := loadJSON(chatStatePath(cfg), &state); err != nil {
		log.Printf("chat: load state: %v", err)
	}
	state.init()

	for {
		now := time.Now()
		targets := []string{cfg.DiscordChannel}
		live := map[string]bool{cfg.DiscordChannel: true}
		if thread := listeningThread(cfg, now); thread != "" {
			targets = append(targets, thread)
			live[thread] = true
		}
		state.prune(now, live)
		for _, channel := range targets {
			if err := pollChannel(cfg, appID, &state, channel, now); err != nil {
				log.Printf("chat: poll %s: %v", channel, err)
				time.Sleep(chatErrorBackoff)
				break
			}
		}
		if err := saveJSON(chatStatePath(cfg), &state); err != nil {
			log.Printf("chat: save state: %v", err)
		}
		time.Sleep(chatPollInterval)
	}
}

func pollChannel(cfg *Config, appID string, state *ChatState, channel string, now time.Time) error {
	cursor := state.Cursors[channel]
	if cursor == "" {
		latest, err := fetchChannel(cfg, channel, "", 1)
		if err != nil {
			return err
		}
		if len(latest) > 0 {
			state.Cursors[channel] = latest[len(latest)-1].ID
		}
		return nil
	}
	messages, err := fetchChannel(cfg, channel, cursor, chatFetchLimit)
	if err != nil {
		return err
	}
	for _, m := range messages {
		state.Cursors[channel] = m.ID
		if m.Author.Bot || strings.TrimSpace(m.Content) == "" {
			continue
		}
		isThread := channel != cfg.DiscordChannel
		if !isThread && (m.Referenced == nil || m.Referenced.Author.ID != appID) {
			continue
		}
		answer(cfg, state, channel, m, isThread, now)
	}
	return nil
}

func answer(cfg *Config, state *ChatState, channel string, m DiscordMessage, isThread bool, now time.Time) {
	question := strings.TrimSpace(m.Content)
	log.Printf("chat: %s in %s: %s", m.Author.Username, channel, question)

	store, err := OpenStore(cfg.StateDir)
	if err != nil {
		replyIn(cfg, channel, m.ID, "error: "+err.Error())
		return
	}
	context := ""
	history := []Turn(nil)
	if isThread {
		if c := state.Conversations[channel]; c != nil {
			history = c.Turns
		}
	} else if m.Referenced != nil {
		context = "the user is replying to this message you sent:\n" + m.Referenced.Content + "\n\n"
	}
	out, err := converse(store, cfg, now, question, context, history)
	store.Close()
	if err != nil {
		replyIn(cfg, channel, m.ID, "error: "+err.Error())
		return
	}

	if isThread {
		c := state.Conversations[channel]
		if c == nil {
			c = &Conversation{}
			state.Conversations[channel] = c
		}
		c.Turns = append(c.Turns, Turn{Question: question, Answer: truncate(out, chatAnswerMaxChars)})
		if len(c.Turns) > chatMaxTurns {
			c.Turns = c.Turns[len(c.Turns)-chatMaxTurns:]
		}
		c.Updated = now
	}
	replyIn(cfg, channel, m.ID, out)
}

// Conversations can act, not just answer: the model may call the same commands
// the slash surface exposes. Results are reported verbatim rather than being
// re-narrated by the model, so what you read is what actually happened.
func converse(store *Store, cfg *Config, now time.Time, question, extra string, history []Turn) (string, error) {
	system := askSystem(now) + "\nWhen the user asks you to DO something, call the matching tool " +
		"instead of describing it. Answer in prose only when they are asking a question."
	content, calls, err := LLMWithTools(cfg, system, askContext(store, now, question, extra, history),
		toolSchemas(), 0.2, 4000)
	if err != nil {
		return "", err
	}
	if len(calls) == 0 {
		return strings.TrimSpace(content), nil
	}
	return runToolCalls(store, cfg, now, calls), nil
}

func fetchChannel(cfg *Config, channel, after string, limit int) ([]DiscordMessage, error) {
	path := fmt.Sprintf("/channels/%s/messages?limit=%d", channel, limit)
	if after != "" {
		path += "&after=" + after
	}
	data, err := discordDo(cfg, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var messages []DiscordMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("chat: decode messages: %w", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func replyIn(cfg *Config, channel, messageID, text string) {
	chunks := chunkMessage(text, discordChunkChars)
	for i, chunk := range chunks {
		payload := map[string]any{"content": chunk}
		if i == 0 {
			payload["message_reference"] = map[string]string{"message_id": messageID}
		}
		if _, err := discordDo(cfg, "POST", "/channels/"+channel+"/messages", payload); err != nil {
			log.Printf("chat: reply: %v", err)
			return
		}
	}
}

// Opens a thread on a message tether posted, so replies there become a conversation.
func OpenThread(cfg *Config, messageID, name string) (string, error) {
	data, err := discordDo(cfg, "POST", "/channels/"+cfg.DiscordChannel+"/messages/"+messageID+"/threads",
		map[string]any{"name": name, "auto_archive_duration": 1440})
	if err != nil {
		return "", err
	}
	var thread struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &thread); err != nil {
		return "", fmt.Errorf("chat: decode thread: %w", err)
	}
	return thread.ID, nil
}
