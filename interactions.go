package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	interactionMaxBody     = 1 << 20
	interactionMaxInFlight = 4
	interactionReadTimeout = 10 * time.Second
)

const (
	interactionPing      = 1
	interactionCommand   = 2
	interactionComponent = 3
	responsePong         = 1
	responseDeferred     = 5
)

const (
	optionString  = 3
	optionInteger = 4
)

type slashOption struct {
	Name        string
	Description string
	Type        int
	Required    bool
	Choices     []string
}

type slashCommand struct {
	Name        string
	Description string
	Options     []slashOption
	// Tool exposes this command to the model in conversations. Bulk operations
	// (sync, triage, pulse, digest) stay off: the timers own those.
	Tool bool
}

// Option order is positional: Execute consumes args in this order, and every
// optional option must come last so a missing value cannot shift the rest.
var slashCommands = []slashCommand{
	{Name: "list", Tool: true, Description: "show tracked threads, commitments, contacts, or reminders", Options: []slashOption{
		{Name: "what", Description: "what to list (default threads)", Type: optionString,
			Choices: []string{"threads", "commitments", "contacts", "reminders"}},
	}},
	{Name: "ask", Description: "ask a question about your mail, commitments, and calendar", Options: []slashOption{
		{Name: "question", Description: "what you want to know", Type: optionString, Required: true},
	}},
	{Name: "show", Tool: true, Description: "read a thread's latest message", Options: []slashOption{
		{Name: "id", Description: "thread id from /list", Type: optionString, Required: true},
	}},
	{Name: "done", Tool: true, Description: "close a thread, keep a commitment, or finish a reminder", Options: []slashOption{
		{Name: "id", Description: "id from /list", Type: optionString, Required: true},
	}},
	{Name: "snooze", Tool: true, Description: "hide a thread for a while", Options: []slashOption{
		{Name: "id", Description: "thread id from /list", Type: optionString, Required: true},
		{Name: "duration", Description: "3d, 12h, ...", Type: optionString, Required: true},
	}},
	{Name: "remind", Tool: true, Description: "add a reminder", Options: []slashOption{
		{Name: "text", Description: "what to remember", Type: optionString, Required: true},
		{Name: "when", Description: "YYYY-MM-DD, today, tomorrow, or 3d", Type: optionString, Required: true},
	}},
	{Name: "event", Tool: true, Description: "put something on the tether calendar, with reminders", Options: []slashOption{
		{Name: "summary", Description: "what it is", Type: optionString, Required: true},
		{Name: "when", Description: "2026-09-01 14:30, tomorrow 3pm, 9am", Type: optionString, Required: true},
		{Name: "minutes", Description: "how long (default 60)", Type: optionInteger},
	}},
	{Name: "suggest", Tool: true, Description: "suggest people worth tracking, ranked by who you actually reply to"},
	{Name: "track", Tool: true, Description: "track a contact so you get nudged when you go quiet", Options: []slashOption{
		{Name: "email", Description: "their email address", Type: optionString, Required: true},
		{Name: "cadence", Description: "days between contact (default 60)", Type: optionInteger},
	}},
	{Name: "digest", Description: "post the digest now"},
	{Name: "sync", Description: "fetch new mail and calendar now"},
	{Name: "triage", Description: "classify queued threads now"},
	{Name: "pulse", Description: "sync, triage, and evaluate nudges now"},
}

type interaction struct {
	Type      int    `json:"type"`
	Token     string `json:"token"`
	ChannelID string `json:"channel_id"`
	Data      struct {
		Name     string `json:"name"`
		CustomID string `json:"custom_id"`
		Options  []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"options"`
	} `json:"data"`
}

func RunInteractions(cfg *Config) error {
	publicKey, err := hex.DecodeString(cfg.DiscordPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("bot: TETHER_DISCORD_PUBLIC_KEY must be %d hex bytes", ed25519.PublicKeySize)
	}
	appID, err := discordAppID(cfg)
	if err != nil {
		return err
	}
	if err := registerCommands(cfg, appID); err != nil {
		return err
	}

	go RunChat(cfg, appID)

	slots := make(chan struct{}, interactionMaxInFlight)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveInteraction(cfg, appID, ed25519.PublicKey(publicKey), slots, w, r)
	})
	server := &http.Server{Addr: cfg.BotAddr, Handler: mux, ReadTimeout: interactionReadTimeout}
	log.Printf("bot: interactions endpoint on %s (app %s)", cfg.BotAddr, appID)
	return server.ListenAndServe()
}

func serveInteraction(cfg *Config, appID string, publicKey ed25519.PublicKey, slots chan struct{},
	w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, interactionMaxBody))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !verifySignature(publicKey, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body) {
		http.Error(w, "invalid request signature", http.StatusUnauthorized)
		return
	}
	var in interaction
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "bad interaction", http.StatusBadRequest)
		return
	}

	switch in.Type {
	case interactionPing:
		writeJSON(w, map[string]int{"type": responsePong})
	case interactionCommand, interactionComponent:
		if in.ChannelID != cfg.DiscordChannel {
			log.Printf("bot: ignoring /%s from channel %s", in.Data.Name, in.ChannelID)
			writeJSON(w, map[string]any{"type": 4, "data": map[string]any{
				"content": "tether only answers in its own channel", "flags": 64}})
			return
		}
		writeJSON(w, map[string]int{"type": responseDeferred})
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				runInteraction(cfg, appID, in)
			}()
		default:
			followUp(cfg, appID, in.Token, "too many commands in flight, try again in a moment")
		}
	default:
		http.Error(w, "unsupported interaction", http.StatusBadRequest)
	}
}

func verifySignature(publicKey ed25519.PublicKey, signature, timestamp string, body []byte) bool {
	if signature == "" || timestamp == "" {
		return false
	}
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, append([]byte(timestamp), body...), sig)
}

func runInteraction(cfg *Config, appID string, in interaction) {
	command, args := interactionArgs(in)
	log.Printf("bot: /%s %v", command, args)

	store, err := OpenStore(cfg.StateDir)
	if err != nil {
		followUp(cfg, appID, in.Token, "error: "+err.Error())
		return
	}
	if command == "suggest" {
		text, buttons := cmdSuggest(store, cfg, time.Now())
		store.Close()
		followUpButtons(cfg, appID, in.Token, text, buttons)
		return
	}
	out, err := Execute(store, cfg, time.Now(), command, args)
	store.Close()
	if err != nil {
		followUp(cfg, appID, in.Token, "error: "+err.Error())
		return
	}
	if strings.TrimSpace(out) == "" {
		out = "ok"
	}
	out = strings.TrimRight(out, "\n")
	if command == "list" {
		postFollowUp(cfg, appID, in.Token, fenceChunks(out))
		return
	}
	followUp(cfg, appID, in.Token, out)
}

// Button custom ids carry the command directly: "snooze:a1b2c3d4:3d".
func interactionArgs(in interaction) (string, []string) {
	if in.Type == interactionComponent {
		parts := strings.Split(in.Data.CustomID, ":")
		return parts[0], parts[1:]
	}
	values := make(map[string]string, len(in.Data.Options))
	for _, o := range in.Data.Options {
		values[o.Name] = optionValue(o.Value)
	}
	return in.Data.Name, orderedArgs(in.Data.Name, values)
}

// Named values become the positional args Execute expects, in declaration order.
func orderedArgs(command string, values map[string]string) []string {
	var args []string
	for _, spec := range slashCommands {
		if spec.Name != command {
			continue
		}
		for _, option := range spec.Options {
			if v, ok := values[option.Name]; ok && strings.TrimSpace(v) != "" {
				args = append(args, v)
			}
		}
	}
	return args
}

func optionValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func followUp(cfg *Config, appID, token, text string) {
	postFollowUp(cfg, appID, token, chunkMessage(text, discordChunkChars))
}

func followUpButtons(cfg *Config, appID, token, text string, buttons []map[string]any) {
	chunks := chunkMessage(text, discordChunkChars)
	if len(chunks) == 0 {
		return
	}
	postFollowUp(cfg, appID, token, chunks[:len(chunks)-1])
	payload := map[string]any{"content": chunks[len(chunks)-1]}
	if len(buttons) > 0 {
		payload["components"] = []map[string]any{{"type": 1, "components": buttons}}
	}
	path := "/webhooks/" + appID + "/" + token
	if len(chunks) == 1 {
		path += "/messages/@original"
		if _, err := discordDo(cfg, "PATCH", path, payload); err != nil {
			log.Printf("bot: follow-up: %v", err)
		}
		return
	}
	if _, err := discordDo(cfg, "POST", path, payload); err != nil {
		log.Printf("bot: follow-up: %v", err)
	}
}

func postFollowUp(cfg *Config, appID, token string, chunks []string) {
	if len(chunks) == 0 {
		return
	}
	path := "/webhooks/" + appID + "/" + token + "/messages/@original"
	if _, err := discordDo(cfg, "PATCH", path, map[string]string{"content": chunks[0]}); err != nil {
		log.Printf("bot: follow-up: %v", err)
		return
	}
	for _, chunk := range chunks[1:] {
		if _, err := discordDo(cfg, "POST", "/webhooks/"+appID+"/"+token, map[string]string{"content": chunk}); err != nil {
			log.Printf("bot: follow-up: %v", err)
			return
		}
	}
}

func discordAppID(cfg *Config) (string, error) {
	data, err := discordDo(cfg, "GET", "/applications/@me", nil)
	if err != nil {
		return "", err
	}
	var app struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &app); err != nil || app.ID == "" {
		return "", fmt.Errorf("bot: could not read application id: %w", err)
	}
	return app.ID, nil
}

// Guild-scoped commands appear instantly; global ones can take an hour to
// propagate, so prefer the guild whenever the bot is in exactly one.
func registerCommands(cfg *Config, appID string) error {
	payload := make([]map[string]any, 0, len(slashCommands))
	for _, command := range slashCommands {
		options := make([]map[string]any, 0, len(command.Options))
		for _, o := range command.Options {
			option := map[string]any{
				"name": o.Name, "description": o.Description, "type": o.Type, "required": o.Required,
			}
			if len(o.Choices) > 0 {
				choices := make([]map[string]string, 0, len(o.Choices))
				for _, c := range o.Choices {
					choices = append(choices, map[string]string{"name": c, "value": c})
				}
				option["choices"] = choices
			}
			options = append(options, option)
		}
		payload = append(payload, map[string]any{
			"name": command.Name, "description": command.Description, "options": options,
		})
	}

	scope := "/applications/" + appID + "/commands"
	if guildID, err := soleGuild(cfg); err != nil {
		log.Printf("bot: guild lookup failed, registering globally: %v", err)
	} else if guildID != "" {
		scope = "/applications/" + appID + "/guilds/" + guildID + "/commands"
	}
	if _, err := discordDo(cfg, "PUT", scope, payload); err != nil {
		return err
	}
	log.Printf("bot: registered %d commands at %s", len(payload), scope)
	return nil
}

func soleGuild(cfg *Config) (string, error) {
	data, err := discordDo(cfg, "GET", "/users/@me/guilds", nil)
	if err != nil {
		return "", err
	}
	var guilds []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &guilds); err != nil {
		return "", err
	}
	if len(guilds) != 1 {
		return "", nil
	}
	return guilds[0].ID, nil
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("bot: write response: %v", err)
	}
}
