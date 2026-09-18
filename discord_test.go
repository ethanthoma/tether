package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestChunkMessage(t *testing.T) {
	chunks := chunkMessage("one\ntwo", discordChunkChars)
	if len(chunks) != 1 || chunks[0] != "one\ntwo" {
		t.Fatalf("short message split: %v", chunks)
	}
	if got := chunkMessage("   ", discordChunkChars); got != nil {
		t.Fatalf("blank message = %v, want nil", got)
	}
	long := strings.Repeat("line of text\n", 400)
	chunks = chunkMessage(long, discordChunkChars)
	if len(chunks) < 2 {
		t.Fatalf("long message not split: %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > discordChunkChars+len("\n… truncated") {
			t.Fatalf("chunk %d is %d chars, over the limit", i, len(c))
		}
	}
}

func TestChunkMessageBounded(t *testing.T) {
	chunks := chunkMessage(strings.Repeat("x", discordChunkChars*(discordMaxChunks+5)), discordChunkChars)
	if len(chunks) != discordMaxChunks {
		t.Fatalf("got %d chunks, want %d", len(chunks), discordMaxChunks)
	}
	if !strings.HasSuffix(chunks[len(chunks)-1], "… truncated") {
		t.Fatal("overflow was dropped without a truncation marker")
	}
}

func TestVerifySignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":1}`)
	timestamp := "1787600120"
	signature := hex.EncodeToString(ed25519.Sign(private, append([]byte(timestamp), body...)))

	if !verifySignature(public, signature, timestamp, body) {
		t.Fatal("valid signature rejected")
	}
	if verifySignature(public, signature, "1787600121", body) {
		t.Fatal("signature accepted for a different timestamp")
	}
	if verifySignature(public, signature, timestamp, []byte(`{"type":2}`)) {
		t.Fatal("signature accepted for a tampered body")
	}
	if verifySignature(public, "", timestamp, body) {
		t.Fatal("empty signature accepted")
	}
	if verifySignature(public, "not-hex", timestamp, body) {
		t.Fatal("malformed signature accepted")
	}
}

func TestInteractionArgsAreOrderedPositionally(t *testing.T) {
	cases := []struct {
		raw     string
		command string
		args    []string
	}{
		{`{"data":{"name":"list","options":[{"name":"what","value":"commitments"}]}}`, "list", []string{"commitments"}},
		{`{"data":{"name":"list"}}`, "list", nil},
		{`{"data":{"name":"remind","options":[{"name":"when","value":"tomorrow"},{"name":"text","value":"buy milk"}]}}`,
			"remind", []string{"buy milk", "tomorrow"}},
		{`{"data":{"name":"track","options":[{"name":"email","value":"a@b.c"},{"name":"cadence","value":30}]}}`,
			"track", []string{"a@b.c", "30"}},
		{`{"data":{"name":"snooze","options":[{"name":"duration","value":"3d"},{"name":"id","value":"a1b2"}]}}`,
			"snooze", []string{"a1b2", "3d"}},
	}
	for _, c := range cases {
		var in interaction
		if err := json.Unmarshal([]byte(c.raw), &in); err != nil {
			t.Fatalf("unmarshal %s: %v", c.raw, err)
		}
		command, args := interactionArgs(in)
		if command != c.command {
			t.Errorf("command = %q, want %q", command, c.command)
		}
		if strings.Join(args, "|") != strings.Join(c.args, "|") {
			t.Errorf("%s args = %v, want %v", c.command, args, c.args)
		}
	}
}

func TestSlashCommandsHaveOptionalOptionsLast(t *testing.T) {
	for _, command := range slashCommands {
		seenOptional := false
		for _, option := range command.Options {
			if !option.Required && !seenOptional {
				seenOptional = true
				continue
			}
			if option.Required && seenOptional {
				t.Errorf("/%s: required option %q follows an optional one, which shifts positional args",
					command.Name, option.Name)
			}
		}
	}
}

func TestCleanStripsMarkdownAndNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**IMPORTANT** offer", "IMPORTANT offer"},
		{"re: `code` thing", "re: code thing"},
		{"subject\nwith\r\nbreaks", "subject with breaks"},
		{"__underline__ and ~~strike~~", "underline and strike"},
		{"  padded  ", "padded"},
	}
	for _, c := range cases {
		if got := clean(c.in); got != c.want {
			t.Errorf("clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestThreadRankPutsActionableFirst(t *testing.T) {
	now := time.Now()
	ordered := []ThreadState{ThreadNeedsReply, ThreadWaitingOnThem, ThreadNew, ThreadFYI}
	for i := 1; i < len(ordered); i++ {
		before := &Thread{State: ordered[i-1]}
		after := &Thread{State: ordered[i]}
		if threadRank(before, now) >= threadRank(after, now) {
			t.Errorf("%s should rank before %s", ordered[i-1], ordered[i])
		}
	}
	snoozed := &Thread{State: ThreadNeedsReply, SnoozeUntil: now.Add(time.Hour)}
	if threadRank(snoozed, now) <= threadRank(&Thread{State: ThreadFYI}, now) {
		t.Error("a snoozed thread should sort after everything active")
	}
	if threadGroup(snoozed, now) != "snoozed" {
		t.Errorf("snoozed group = %q", threadGroup(snoozed, now))
	}
}

func TestListLinesFitAPhone(t *testing.T) {
	store := &Store{Threads: []*Thread{
		{ID: "a", State: ThreadNeedsReply, Subject: "A really quite long subject line that would wrap badly"},
		{ID: "b", State: ThreadNew, Subject: "short"},
	}}
	out, err := cmdList(store, time.Now(), "threads")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 32 {
			t.Errorf("line is %d cols, want <= 32: %q", len([]rune(line)), line)
		}
	}
	if !strings.Contains(out, "reply\n") || !strings.Contains(out, "new\n") {
		t.Errorf("expected state group headers, got:\n%s", out)
	}
}

func TestCleanDropsEmoji(t *testing.T) {
	cases := []struct{ in, want string }{
		{"🔥 Back to School Sale", "Back to School Sale"},
		{"🚨 Our First Slab", "Our First Slab"},
		{"☕️ Weekend to-do", "Weekend to-do"},
		{"no emoji here", "no emoji here"},
		{"A 🔥 B", "A B"},
		{"café — naïve résumé", "café — naïve résumé"},
		{"5 > 3 and 2 < 4", "5 3 and 2 < 4"},
	}
	for _, c := range cases {
		if got := clean(c.in); got != c.want {
			t.Errorf("clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseEventTime(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.Local)
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2026-09-01 14:30", time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local)},
		{"tomorrow 3pm", time.Date(2026, 8, 25, 15, 0, 0, 0, time.Local)},
		{"today 09:00", time.Date(2026, 8, 24, 9, 0, 0, 0, time.Local)},
		{"tomorrow 9:30am", time.Date(2026, 8, 25, 9, 30, 0, 0, time.Local)},
		{"2026-09-01", time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local)},
		{"3pm", time.Date(2026, 8, 24, 15, 0, 0, 0, time.Local)},
		{"9am", time.Date(2026, 8, 25, 9, 0, 0, 0, time.Local)}, // already past today
	}
	for _, c := range cases {
		got, err := parseEventTime(now, c.in)
		if err != nil {
			t.Errorf("parseEventTime(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseEventTime(%q) = %s, want %s", c.in, got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
	for _, bad := range []string{"", "nonsense", "2026-13-45 10:00", "tomorrow 25:00", "a b c"} {
		if got, err := parseEventTime(now, bad); err == nil {
			t.Errorf("parseEventTime(%q) = %s, want an error", bad, got)
		}
	}
}

func TestEventArgSplit(t *testing.T) {
	now := time.Date(2026, 8, 24, 13, 0, 0, 0, time.Local)
	cases := []struct {
		args    []string
		summary string
		start   time.Time
		minutes int
	}{
		{[]string{"tether calendar test", "tomorrow", "10am", "30"},
			"tether calendar test", time.Date(2026, 8, 25, 10, 0, 0, 0, time.Local), 30},
		{[]string{"dinner", "with", "sam", "tomorrow", "7pm"},
			"dinner with sam", time.Date(2026, 8, 25, 19, 0, 0, 0, time.Local), 60},
		{[]string{"standup", "9am"},
			"standup", time.Date(2026, 8, 25, 9, 0, 0, 0, time.Local), 60},
		{[]string{"review", "2026-09-01", "14:30", "45"},
			"review", time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local), 45},
	}
	for _, c := range cases {
		summary, start, minutes, err := splitEventArgs(now, c.args)
		if err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if summary != c.summary {
			t.Errorf("%v summary = %q, want %q", c.args, summary, c.summary)
		}
		if !start.Equal(c.start) {
			t.Errorf("%v start = %s, want %s", c.args, start.Format(time.RFC3339), c.start.Format(time.RFC3339))
		}
		if minutes != c.minutes {
			t.Errorf("%v minutes = %d, want %d", c.args, minutes, c.minutes)
		}
	}
	if _, _, _, err := splitEventArgs(now, []string{"tomorrow", "9am"}); err == nil {
		t.Error("want an error when there is no summary")
	}
}

func TestIntroProseRejectsModelDrift(t *testing.T) {
	drifted := "Here is your briefing!\n\n**needs your reply**\n- `b7ba3564` Application @ The Raven\n" +
		"📅 Next 48h\n- Tue 10:30 Gym\n\nWhat would you like to tackle first?"
	got := introProse(drifted)
	for _, bad := range []string{"- ", "**", "📅", "?"} {
		if strings.Contains(got, bad) {
			t.Errorf("intro kept %q: %q", bad, got)
		}
	}
	if got == "" {
		t.Fatal("intro dropped everything, want the prose line kept")
	}

	good := "the raven application has been sitting three days. clear it today."
	if introProse(good) != good {
		t.Errorf("clean prose altered: %q", introProse(good))
	}
	if introProse("- only\n- bullets\n") != "" {
		t.Error("all-bullet output should yield no intro")
	}
	if len([]rune(introProse(strings.Repeat("long sentence here. ", 100)))) > introMaxChars {
		t.Error("intro exceeded the char cap")
	}
}

func TestComposeDigestPutsHeaderFirst(t *testing.T) {
	body := "**tether digest — Mon Aug 24**\n\n**needs your reply**\n- `a1b2` thing — 3d\n"
	out := composeDigest(body, "one thing matters today.")
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "**tether digest") {
		t.Fatalf("first line = %q, want the header", lines[0])
	}
	if !strings.Contains(out, "**tether digest — Mon Aug 24**\n\none thing matters today.\n") {
		t.Errorf("intro is not directly under the header:\n%s", out)
	}
	if !strings.Contains(out, "**needs your reply**") {
		t.Error("sections were lost")
	}
	if got := composeDigest(body, ""); got != body {
		t.Error("empty intro should leave the digest untouched")
	}
}

func TestComponentInteractionArgs(t *testing.T) {
	cases := []struct {
		customID string
		command  string
		args     []string
	}{
		{"done:a1b2c3d4", "done", []string{"a1b2c3d4"}},
		{"snooze:a1b2c3d4:3d", "snooze", []string{"a1b2c3d4", "3d"}},
		{"show:a1b2c3d4", "show", []string{"a1b2c3d4"}},
	}
	for _, c := range cases {
		in := interaction{Type: interactionComponent}
		in.Data.CustomID = c.customID
		command, args := interactionArgs(in)
		if command != c.command || strings.Join(args, "|") != strings.Join(c.args, "|") {
			t.Errorf("%q -> %q %v, want %q %v", c.customID, command, args, c.command, c.args)
		}
	}
}

func TestNudgeButtons(t *testing.T) {
	thread := Nudge{Target: "a1b2c3d4", Thread: true}
	if got := len(thread.buttons()); got != 3 {
		t.Errorf("thread nudge has %d buttons, want 3", got)
	}
	item := Nudge{Target: "commit123"}
	if got := len(item.buttons()); got != 1 {
		t.Errorf("item nudge has %d buttons, want 1 (nothing else applies)", got)
	}
	if (Nudge{}).buttons() != nil {
		t.Error("a nudge with no target should carry no buttons")
	}
	for _, b := range thread.buttons() {
		id := b["custom_id"].(string)
		if len(id) > 100 {
			t.Errorf("custom_id over Discord's 100-char limit: %q", id)
		}
		command, args := func() (string, []string) {
			in := interaction{Type: interactionComponent}
			in.Data.CustomID = id
			return interactionArgs(in)
		}()
		if _, err := Execute(&Store{}, &Config{}, time.Now(), command, args); err == nil {
			t.Errorf("%q should reach a real command (empty store errors, but not on usage)", id)
		} else if strings.HasPrefix(err.Error(), "usage:") || strings.HasPrefix(err.Error(), "unknown command") {
			t.Errorf("button %q does not map to a valid command: %v", id, err)
		}
	}
}

func TestChatStatePruneIsBounded(t *testing.T) {
	now := time.Now()
	state := &ChatState{}
	state.init()
	state.Cursors["channel"] = "1"
	state.Cursors["dead-thread"] = "2"
	state.Conversations["channel"] = &Conversation{Updated: now}
	state.Conversations["dead-thread"] = &Conversation{Updated: now}
	state.Conversations["stale"] = &Conversation{Updated: now.Add(-2 * chatConversationTTL)}

	state.prune(now, map[string]bool{"channel": true})

	if _, ok := state.Cursors["dead-thread"]; ok {
		t.Error("cursor for an unwatched channel survived")
	}
	if _, ok := state.Conversations["dead-thread"]; ok {
		t.Error("conversation for an unwatched channel survived")
	}
	if _, ok := state.Conversations["stale"]; ok {
		t.Error("expired conversation survived")
	}
	if _, ok := state.Cursors["channel"]; !ok {
		t.Error("live channel cursor was dropped")
	}
}

func TestConversationKeepsOnlyRecentTurns(t *testing.T) {
	c := &Conversation{}
	for i := 0; i < chatMaxTurns+4; i++ {
		c.Turns = append(c.Turns, Turn{Question: fmt.Sprintf("q%d", i)})
		if len(c.Turns) > chatMaxTurns {
			c.Turns = c.Turns[len(c.Turns)-chatMaxTurns:]
		}
	}
	if len(c.Turns) != chatMaxTurns {
		t.Fatalf("kept %d turns, want %d", len(c.Turns), chatMaxTurns)
	}
	if c.Turns[len(c.Turns)-1].Question != "q6" {
		t.Errorf("last turn = %q, want the newest", c.Turns[len(c.Turns)-1].Question)
	}
}

func TestToolSchemasMatchCommands(t *testing.T) {
	schemas := toolSchemas()
	if len(schemas) == 0 {
		t.Fatal("no tools generated")
	}
	exposed := map[string]bool{}
	for _, s := range schemas {
		fn := s["function"].(map[string]any)
		exposed[fn["name"].(string)] = true
		params := fn["parameters"].(map[string]any)
		props := params["properties"].(map[string]any)
		for _, spec := range slashCommands {
			if spec.Name != fn["name"].(string) {
				continue
			}
			if len(props) != len(spec.Options) {
				t.Errorf("%s exposes %d params, command has %d options", spec.Name, len(props), len(spec.Options))
			}
			for _, o := range spec.Options {
				if _, ok := props[o.Name]; !ok {
					t.Errorf("%s is missing param %q", spec.Name, o.Name)
				}
			}
		}
	}
	for _, bulk := range []string{"sync", "triage", "pulse", "digest"} {
		if exposed[bulk] {
			t.Errorf("%q should not be callable by the model", bulk)
		}
	}
	for _, want := range []string{"done", "snooze", "remind", "event", "track", "list", "show"} {
		if !exposed[want] {
			t.Errorf("%q should be callable by the model", want)
		}
	}
}

func TestRunToolCallsRefusesUnknownTools(t *testing.T) {
	call := ToolCall{}
	call.Function.Name = "pulse"
	call.Function.Arguments = "{}"
	out := runToolCalls(&Store{}, &Config{}, time.Now(), []ToolCall{call})
	if !strings.Contains(out, "refused") {
		t.Errorf("a non-tool command should be refused, got: %q", out)
	}
}

func TestRunToolCallsIsBounded(t *testing.T) {
	var calls []ToolCall
	for i := 0; i < toolMaxCalls+3; i++ {
		call := ToolCall{}
		call.Function.Name = "list"
		call.Function.Arguments = `{"what":"reminders"}`
		calls = append(calls, call)
	}
	out := runToolCalls(&Store{}, &Config{}, time.Now(), calls)
	if !strings.Contains(out, "stopped after") {
		t.Errorf("expected a cap notice, got: %q", out)
	}
}

func TestToolArgumentsBecomePositionalArgs(t *testing.T) {
	args := orderedArgs("event", map[string]string{"when": "tomorrow 3pm", "summary": "dinner", "minutes": "90"})
	if strings.Join(args, "|") != "dinner|tomorrow 3pm|90" {
		t.Errorf("args = %v, want summary, when, minutes in order", args)
	}
	args = orderedArgs("track", map[string]string{"email": "a@b.c"})
	if strings.Join(args, "|") != "a@b.c" {
		t.Errorf("args = %v, want just the email", args)
	}
}

func TestSuggestRanksRealCorrespondents(t *testing.T) {
	now := time.Now()
	cfg := &Config{MyEmail: "me@example.com"}
	store := &Store{Threads: []*Thread{
		{ID: "1", State: ThreadNeedsReply, Participants: []string{"me@example.com", "alice@example.com"},
			LastInbound: now, LastOutbound: now},
		{ID: "2", State: ThreadWaitingOnThem, Participants: []string{"alice@example.com"},
			LastInbound: now, LastOutbound: now},
		{ID: "3", State: ThreadFYI, Participants: []string{"bob@example.com"}, LastInbound: now},
		{ID: "4", State: ThreadFYI, Participants: []string{"bob@example.com"}, LastInbound: now},
		{ID: "5", State: ThreadFYI, Participants: []string{"noreply@github.com"}, LastInbound: now},
		{ID: "6", State: ThreadNoise, Participants: []string{"spam@marketing.com"}, LastInbound: now},
		{ID: "7", State: ThreadFYI, Participants: []string{"onceoff@example.com"}, LastInbound: now},
	}}

	got := suggestContacts(store, cfg)
	if len(got) < 2 {
		t.Fatalf("expected alice and bob, got %+v", got)
	}
	if got[0].Email != "alice@example.com" {
		t.Errorf("top candidate = %s, want alice (replied to twice)", got[0].Email)
	}
	for _, c := range got {
		switch c.Email {
		case "me@example.com":
			t.Error("the user should never be suggested")
		case "noreply@github.com":
			t.Error("automated senders should be filtered")
		case "spam@marketing.com":
			t.Error("noise threads should not produce candidates")
		case "onceoff@example.com":
			t.Error("a single unanswered thread is not a relationship")
		}
	}
}

func TestSuggestSkipsAlreadyTracked(t *testing.T) {
	now := time.Now()
	cfg := &Config{MyEmail: "me@example.com"}
	store := &Store{
		Threads: []*Thread{{ID: "1", State: ThreadFYI, Participants: []string{"alice@example.com"},
			LastInbound: now, LastOutbound: now}},
		Contacts: []*Contact{{Email: "alice@example.com", Tracked: true, CadenceDays: 30}},
	}
	if got := suggestContacts(store, cfg); len(got) != 0 {
		t.Errorf("already-tracked contact was suggested again: %+v", got)
	}
}

func TestSuggestButtonsAreValidTrackCommands(t *testing.T) {
	now := time.Now()
	cfg := &Config{MyEmail: "me@example.com"}
	store := &Store{Threads: []*Thread{
		{ID: "1", State: ThreadFYI, Participants: []string{"alice@example.com"}, LastInbound: now, LastOutbound: now},
	}}
	_, buttons := cmdSuggest(store, cfg, now)
	if len(buttons) == 0 {
		t.Fatal("expected a track button")
	}
	for _, b := range buttons {
		id := b["custom_id"].(string)
		if len(id) > 100 {
			t.Errorf("custom_id too long: %q", id)
		}
		in := interaction{Type: interactionComponent}
		in.Data.CustomID = id
		command, args := interactionArgs(in)
		if command != "track" || len(args) != 2 {
			t.Errorf("button %q -> %q %v, want track with email and cadence", id, command, args)
		}
	}
}

func TestRoleAddressesNeedAReply(t *testing.T) {
	now := time.Now()
	cfg := &Config{MyEmail: "me@example.com"}
	store := &Store{Threads: []*Thread{
		{ID: "1", State: ThreadFYI, Participants: []string{"service@intl.paypal.com"}, LastInbound: now},
		{ID: "2", State: ThreadFYI, Participants: []string{"service@intl.paypal.com"}, LastInbound: now},
		{ID: "3", State: ThreadFYI, Participants: []string{"shop@t.englandstore.com"}, LastInbound: now},
		{ID: "4", State: ThreadFYI, Participants: []string{"shop@t.englandstore.com"}, LastInbound: now},
		{ID: "5", State: ThreadFYI, Participants: []string{"ellen@gmail.com"}, LastInbound: now},
		{ID: "6", State: ThreadFYI, Participants: []string{"ellen@gmail.com"}, LastInbound: now},
		{ID: "7", State: ThreadFYI, Participants: []string{"billing@landlord.com"},
			LastInbound: now, LastOutbound: now},
	}}
	got := suggestContacts(store, cfg)
	found := map[string]bool{}
	for _, c := range got {
		found[c.Email] = true
	}
	if found["service@intl.paypal.com"] || found["shop@t.englandstore.com"] {
		t.Errorf("unanswered role addresses were suggested: %+v", got)
	}
	if !found["ellen@gmail.com"] {
		t.Error("a personal address with repeat contact should be suggested")
	}
	if !found["billing@landlord.com"] {
		t.Error("a role address you replied to should still be suggested")
	}
}

func TestRoleAddress(t *testing.T) {
	for _, role := range []string{"service@paypal.com", "shop@store.com", "billing@x.com", "no-reply@y.com"} {
		if !roleAddress(role) && !automated(role) {
			t.Errorf("%q should be treated as a role or automated address", role)
		}
	}
	for _, person := range []string{"ellenjeong3532@gmail.com", "dalethoma@gmail.com", "j.smith@corp.com"} {
		if roleAddress(person) {
			t.Errorf("%q should be treated as a person", person)
		}
	}
}
