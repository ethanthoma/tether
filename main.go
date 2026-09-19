package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	StateDir           string
	IMAPUser           string
	IMAPPassword       string
	MyEmail            string
	ICSURL             string
	DiscordToken       string
	DiscordChannel     string
	DiscordPublicKey   string
	BotAddr            string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRefreshToken string
	LLMURL             string
	LLMKey             string
	BendShadow         string
	BendDispatch       string
	BendDelivery       string
	TriageShadow       string
}

func loadConfig() *Config {
	env := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	cfg := &Config{
		StateDir:           env("TETHER_STATE_DIR", "/var/lib/tether"),
		IMAPUser:           os.Getenv("TETHER_IMAP_USER"),
		IMAPPassword:       os.Getenv("TETHER_IMAP_PASSWORD"),
		ICSURL:             os.Getenv("TETHER_ICS_URL"),
		DiscordToken:       os.Getenv("TETHER_DISCORD_TOKEN"),
		DiscordChannel:     os.Getenv("TETHER_DISCORD_CHANNEL"),
		DiscordPublicKey:   os.Getenv("TETHER_DISCORD_PUBLIC_KEY"),
		BotAddr:            env("TETHER_BOT_ADDR", "127.0.0.1:8082"),
		GoogleClientID:     os.Getenv("TETHER_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("TETHER_GOOGLE_CLIENT_SECRET"),
		GoogleRefreshToken: os.Getenv("TETHER_GOOGLE_REFRESH_TOKEN"),
		LLMURL:             env("TETHER_LLM_URL", "http://127.0.0.1:8080"),
		LLMKey:             os.Getenv("LLAMA_API_KEY"),
		BendShadow:         os.Getenv("TETHER_BEND_SHADOW"),
		BendDispatch:       os.Getenv("TETHER_BEND_DISPATCH"),
		BendDelivery:       os.Getenv("TETHER_BEND_DELIVERY"),
		TriageShadow:       os.Getenv("TETHER_TRIAGE_SHADOW"),
	}
	cfg.MyEmail = env("TETHER_MY_EMAIL", cfg.IMAPUser)
	return cfg
}

func (cfg *Config) need(fields map[string]string) error {
	var missing []string
	for name, value := range fields {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
}

func (cfg *Config) needDiscord() error {
	return cfg.need(map[string]string{
		"TETHER_DISCORD_TOKEN":   cfg.DiscordToken,
		"TETHER_DISCORD_CHANNEL": cfg.DiscordChannel,
	})
}

const usage = `usage: tether <command>
  sync                       fetch new mail (IMAP) and calendar (ICS)
  triage                     LLM-classify new threads, extract commitments
  triage-shadow              read-only local classifier predictions
  nudge                      evaluate nudge rules, post to Discord
  shadow                     compare Go/Bend eligibility without sending
  shadow-dispatch            compare Go/Bend batch sizes without sending
  digest                     post morning digest to Discord
  pulse                      sync + triage + nudge (what the timer runs)
  bot                        serve Discord slash commands (long-running)
  ask "<question>"           ask the LLM a question over the store
  list [threads|commitments|contacts|reminders]
  show <id>                  print a thread's latest message body
  done <id>                  close a thread / keep a commitment / finish a reminder
  snooze <id> <dur>          snooze a thread (e.g. 3d, 12h)
  suggest                    rank people worth tracking, from who you reply to
  track <email> [cadence-days]   track a contact for staleness (default 60)
  remind "<text>" <when>     add a reminder (when: YYYY-MM-DD, today, tomorrow, 3d)
  event "<summary>" <when> [minutes]   add a Google Calendar event with reminders
  auth                       one-time Google consent, prints a refresh token
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cfg := loadConfig()
	command, args := os.Args[1], os.Args[2:]

	if command == "auth" {
		if err := cfg.need(map[string]string{
			"TETHER_GOOGLE_CLIENT_ID":     cfg.GoogleClientID,
			"TETHER_GOOGLE_CLIENT_SECRET": cfg.GoogleClientSecret,
		}); err != nil {
			log.Fatal(err)
		}
		if err := RunAuth(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	if command == "bot" {
		if err := cfg.needDiscord(); err != nil {
			log.Fatal(err)
		}
		if err := cfg.need(map[string]string{
			"TETHER_DISCORD_PUBLIC_KEY": cfg.DiscordPublicKey,
			"LLAMA_API_KEY":             cfg.LLMKey,
		}); err != nil {
			log.Fatal(err)
		}
		log.Fatal(RunInteractions(cfg))
	}

	store, err := OpenStore(cfg.StateDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	out, err := Execute(store, cfg, time.Now(), command, args)
	if err != nil {
		log.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		fmt.Println(strings.TrimRight(out, "\n"))
	}
}

func Execute(store *Store, cfg *Config, now time.Time, command string, args []string) (string, error) {
	switch command {
	case "sync":
		return cmdSync(store, cfg, now)
	case "triage":
		if err := cfg.need(map[string]string{"LLAMA_API_KEY": cfg.LLMKey}); err != nil {
			return "", err
		}
		return cmdTriage(store, cfg, now)
	case "nudge":
		if err := cfg.needDiscord(); err != nil {
			return "", err
		}
		if err := RunNudges(store, cfg, now); err != nil {
			return "", err
		}
		return "nudges evaluated", nil
	case "shadow-dispatch":
		return RunBendDispatchShadow(store, cfg.BendDispatch, now)
	case "triage-shadow":
		return RunTriageShadow(store, cfg.TriageShadow, now)
	case "shadow":
		return RunBendShadow(store, cfg.BendShadow, now)
	case "digest":
		if err := cfg.needDiscord(); err != nil {
			return "", err
		}
		return "", RunDigest(store, cfg, now)
	case "pulse":
		if err := cfg.needDiscord(); err != nil {
			return "", err
		}
		if err := cfg.need(map[string]string{"LLAMA_API_KEY": cfg.LLMKey}); err != nil {
			return "", err
		}
		synced, err := cmdSync(store, cfg, now)
		if err != nil {
			return "", err
		}
		triaged, err := cmdTriage(store, cfg, now)
		if err != nil {
			return "", err
		}
		if err := RunNudges(store, cfg, now); err != nil {
			return "", err
		}
		return synced + "\n" + triaged, nil
	case "ask":
		if err := cfg.need(map[string]string{"LLAMA_API_KEY": cfg.LLMKey}); err != nil {
			return "", err
		}
		if len(args) == 0 {
			return "", fmt.Errorf(`usage: tether ask "<question>"`)
		}
		return cmdAsk(store, cfg, now, strings.Join(args, " "))
	case "list":
		what := "threads"
		if len(args) > 0 {
			what = args[0]
		}
		return cmdList(store, now, what)
	case "done":
		if len(args) < 1 {
			return "", fmt.Errorf("usage: tether done <id>")
		}
		out, err := cmdDone(store, args[0])
		return saving(store, out, err)
	case "snooze":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: tether snooze <id> <dur>")
		}
		out, err := cmdSnooze(store, now, args[0], args[1])
		return saving(store, out, err)
	case "track":
		if len(args) < 1 {
			return "", fmt.Errorf("usage: tether track <email> [cadence-days]")
		}
		out, err := cmdTrack(store, args)
		return saving(store, out, err)
	case "remind":
		if len(args) < 2 {
			return "", fmt.Errorf(`usage: tether remind "<text>" <when>`)
		}
		text := strings.Join(args[:len(args)-1], " ")
		out, err := cmdRemind(store, now, text, args[len(args)-1])
		return saving(store, out, err)
	case "event":
		if len(args) < 2 {
			return "", fmt.Errorf(`usage: tether event "<summary>" <when> [minutes]`)
		}
		if err := cfg.need(map[string]string{
			"TETHER_GOOGLE_CLIENT_ID":     cfg.GoogleClientID,
			"TETHER_GOOGLE_CLIENT_SECRET": cfg.GoogleClientSecret,
			"TETHER_GOOGLE_REFRESH_TOKEN": cfg.GoogleRefreshToken,
		}); err != nil {
			return "", err
		}
		return cmdEvent(store, cfg, now, args)
	case "show":
		if len(args) < 1 {
			return "", fmt.Errorf("usage: tether show <id>")
		}
		return cmdShow(store, args[0])
	case "suggest":
		out, _ := cmdSuggest(store, cfg, now)
		return out, nil
	case "help":
		return usage, nil
	}
	return "", fmt.Errorf("unknown command %q\n%s", command, usage)
}

func saving(store *Store, out string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if err := store.Save(); err != nil {
		return "", err
	}
	return out, nil
}

func cmdSync(store *Store, cfg *Config, now time.Time) (string, error) {
	fields := map[string]string{"TETHER_IMAP_USER": cfg.IMAPUser, "TETHER_IMAP_PASSWORD": cfg.IMAPPassword}
	if err := cfg.need(fields); err != nil {
		return "", err
	}
	added, err := SyncMail(store, cfg, now)
	if err != nil {
		return "", fmt.Errorf("%w (synced %d messages first)", err, added)
	}
	if err := store.Save(); err != nil {
		return "", err
	}
	if cfg.ICSURL != "" {
		events, err := FetchCalendar(cfg.ICSURL, now)
		if err != nil {
			return "", err
		}
		store.Events = events
		store.Sync.ICSFetched = now
		if err := store.Save(); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("sync: %d new messages, %d events", added, len(store.Events)), nil
}

func cmdTriage(store *Store, cfg *Config, now time.Time) (string, error) {
	before := countState(store, ThreadNew)
	if err := RunTriage(store, cfg, now); err != nil {
		return "", err
	}
	done := before - countState(store, ThreadNew)
	return fmt.Sprintf("triage: %d classified, %d still queued", done, countState(store, ThreadNew)), nil
}

func countState(store *Store, state ThreadState) int {
	n := 0
	for _, t := range store.Threads {
		if t.State == state {
			n++
		}
	}
	return n
}

func cmdAsk(store *Store, cfg *Config, now time.Time, question string) (string, error) {
	return askWithHistory(store, cfg, now, question, "", nil)
}

func askWithHistory(store *Store, cfg *Config, now time.Time, question, extra string, history []Turn) (string, error) {
	answer, err := LLMChat(cfg, askSystem(now), askContext(store, now, question, extra, history), 0.2, 4000)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

func askSystem(now time.Time) string {
	return "Reasoning strength: low\nYou answer questions about the user's email threads, commitments, " +
		"reminders, calendar, and contacts using ONLY the provided state. Be terse and concrete; " +
		"reference threads by their `id`. Resolve follow-ups like \"the other one\" against the earlier " +
		"exchanges when they are present. Today is " + now.Format("2006-01-02, Monday") + "."
}

func askContext(store *Store, now time.Time, question, extra string, history []Turn) string {
	context := buildDigest(store, now) + "\nthread details:\n"
	for _, t := range store.Threads {
		if t.State == ThreadNoise || t.State == ThreadDone {
			continue
		}
		context += fmt.Sprintf("`%s` [%s] %s — %s (%s)\n",
			t.ShortID(), t.State, clean(t.Subject), strings.Join(t.Participants, ","), t.TriageNote)
	}
	if extra != "" {
		context += "\n" + extra
	}
	for _, turn := range history {
		context += fmt.Sprintf("\nearlier — you were asked: %s\nyou answered: %s\n", turn.Question, turn.Answer)
	}
	return context + "\nquestion: " + question
}

// Sized for a phone: every line stays under ~32 columns so nothing wraps.
const (
	listMaxLines = 30
	listTextCols = 22
)

func cmdList(store *Store, now time.Time, what string) (string, error) {
	var lines []string
	group := func(label string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, label)
	}
	switch what {
	case "threads":
		sorted := append([]*Thread(nil), store.Threads...)
		sort.Slice(sorted, func(i, j int) bool {
			if rank := threadRank(sorted[i], now) - threadRank(sorted[j], now); rank != 0 {
				return rank < 0
			}
			return latestActivity(sorted[i]).After(latestActivity(sorted[j]))
		})
		current := ""
		for _, t := range sorted {
			if t.State == ThreadNoise || t.State == ThreadDone {
				continue
			}
			if label := threadGroup(t, now); label != current {
				current = label
				group(label)
			}
			lines = append(lines, fmt.Sprintf(" %s %s", t.ShortID(), truncate(clean(t.Subject), listTextCols)))
		}
	case "commitments":
		current := ""
		for _, c := range store.Commitments {
			if string(c.State) != current {
				current = string(c.State)
				group(current)
			}
			due := ""
			if !c.Due.IsZero() {
				due = " " + c.Due.Local().Format("Jan 2")
			}
			lines = append(lines, fmt.Sprintf(" %s %s%s", c.ID, truncate(clean(c.Text), listTextCols), due))
		}
	case "contacts":
		for _, c := range store.Contacts {
			if !c.Tracked {
				continue
			}
			lines = append(lines, fmt.Sprintf(" %s %dd last %s", truncate(clean(c.Email), listTextCols),
				c.CadenceDays, c.LastContactAt.Local().Format("Jan 2")))
		}
	case "reminders":
		for _, r := range store.Reminders {
			lines = append(lines, fmt.Sprintf(" %s %s %s", r.ID, truncate(clean(r.Text), listTextCols),
				r.Due.Local().Format("Jan 2")))
		}
	default:
		return "", fmt.Errorf("unknown list target %q (want threads, commitments, contacts, reminders)", what)
	}
	if len(lines) == 0 {
		return "nothing here", nil
	}
	if len(lines) > listMaxLines {
		hidden := len(lines) - listMaxLines
		lines = append(lines[:listMaxLines], fmt.Sprintf("… %d more", hidden))
	}
	return strings.Join(lines, "\n"), nil
}

func truncate(text string, cols int) string {
	runes := []rune(text)
	if len(runes) <= cols {
		return text
	}
	return strings.TrimSpace(string(runes[:cols-1])) + "…"
}

// Actionable first: an untriaged backlog must never bury what needs a reply.
// Snoozed threads sort last whatever their state, since they are set aside.
func threadRank(t *Thread, now time.Time) int {
	if t.Snoozed(now) {
		return 4
	}
	switch t.State {
	case ThreadNeedsReply:
		return 0
	case ThreadWaitingOnThem:
		return 1
	case ThreadNew:
		return 2
	}
	return 3
}

func threadGroup(t *Thread, now time.Time) string {
	if t.Snoozed(now) {
		return "snoozed"
	}
	switch t.State {
	case ThreadNeedsReply:
		return "reply"
	case ThreadWaitingOnThem:
		return "waiting"
	}
	return string(t.State)
}

// Subjects are attacker-supplied text rendered as markdown in Discord. Emoji go
// too: they render wider than one column and blow the mobile line budget.
func clean(text string) string {
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case isEmoji(r):
			return -1
		}
		return r
	}, text)
	for _, c := range []string{"`", "*", "_", "~", "|", ">"} {
		text = strings.ReplaceAll(text, c, "")
	}
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // pictographs, emoticons, transport, flags
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols and arrows
		return true
	case r >= 0xFE00 && r <= 0xFE0F: // variation selectors
		return true
	case r == 0x200D || r == 0x20E3 || r == 0x3030 || r == 0x303D:
		return true
	}
	return false
}

func latestActivity(t *Thread) time.Time {
	if t.LastOutbound.After(t.LastInbound) {
		return t.LastOutbound
	}
	return t.LastInbound
}

func cmdDone(store *Store, id string) (string, error) {
	if thread, err := store.ThreadByShortID(id); err == nil {
		thread.State = ThreadDone
		return fmt.Sprintf("thread %s done: %s", thread.ShortID(), clean(thread.Subject)), nil
	}
	for _, c := range store.Commitments {
		if c.ID == id {
			c.State = CommitmentKept
			return "commitment kept: " + c.Text, nil
		}
	}
	for _, r := range store.Reminders {
		if r.ID == id {
			r.State = ReminderDone
			return "reminder done: " + r.Text, nil
		}
	}
	return "", fmt.Errorf("nothing matches id %q", id)
}

func cmdSnooze(store *Store, now time.Time, id, duration string) (string, error) {
	thread, err := store.ThreadByShortID(id)
	if err != nil {
		return "", err
	}
	d, err := parseDuration(duration)
	if err != nil {
		return "", err
	}
	thread.SnoozeUntil = now.Add(d)
	return fmt.Sprintf("thread %s snoozed until %s", thread.ShortID(),
		thread.SnoozeUntil.Local().Format("Jan 2 15:04")), nil
}

func cmdTrack(store *Store, args []string) (string, error) {
	email := strings.ToLower(args[0])
	cadence := 60
	if len(args) > 1 {
		v, err := strconv.Atoi(args[1])
		if err != nil || v <= 0 {
			return "", fmt.Errorf("bad cadence %q, want positive days", args[1])
		}
		cadence = v
	}
	c := store.ContactByEmail(email)
	if c == nil {
		c = &Contact{Email: email}
		store.Contacts = append(store.Contacts, c)
	}
	c.Tracked = true
	c.CadenceDays = cadence
	return fmt.Sprintf("tracking %s every %dd", email, cadence), nil
}

const showMaxChars = 1500

func cmdShow(store *Store, id string) (string, error) {
	thread, err := store.ThreadByShortID(id)
	if err != nil {
		return "", err
	}
	if len(thread.MsgIDs) == 0 {
		return "", fmt.Errorf("thread %s has no cached messages", thread.ShortID())
	}
	message, err := store.LoadMessage(thread.MsgIDs[len(thread.MsgIDs)-1])
	if err != nil {
		return "", fmt.Errorf("thread %s: %w", thread.ShortID(), err)
	}
	sender := message.From
	if message.Outbound {
		sender = "you"
	}
	body := strings.ReplaceAll(strings.TrimSpace(message.Body), "```", "'''")
	return fmt.Sprintf("**%s**\nfrom %s, %s\n\n%s",
		clean(thread.Subject), clean(sender), message.Date.Local().Format("Mon Jan 2 15:04"),
		truncate(body, showMaxChars)), nil
}

func cmdEvent(store *Store, cfg *Config, now time.Time, args []string) (string, error) {
	summary, start, minutes, err := splitEventArgs(now, args)
	if err != nil {
		return "", err
	}
	link, err := CreateEvent(store, cfg, summary, start, minutes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s — %s (%dm)\n%s", summary, start.Format("Mon Jan 2 15:04"), minutes, link), nil
}

// "dinner with sam tomorrow 7pm 90" — trailing minutes, then a when that may be
// one or two words, then whatever is left is the summary.
func splitEventArgs(now time.Time, args []string) (string, time.Time, int, error) {
	minutes := eventDefaultMin
	if len(args) > 2 {
		if parsed, err := strconv.Atoi(args[len(args)-1]); err == nil && parsed > 0 {
			minutes = parsed
			args = args[:len(args)-1]
		}
	}
	when, summaryEnd := args[len(args)-1], len(args)-1
	if len(args) >= 2 {
		if joined := args[len(args)-2] + " " + when; validEventTime(now, joined) {
			when, summaryEnd = joined, len(args)-2
		}
	}
	start, err := parseEventTime(now, when)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	summary := strings.Join(args[:summaryEnd], " ")
	if summary == "" {
		return "", time.Time{}, 0, fmt.Errorf("event needs a summary before the time")
	}
	return summary, start, minutes, nil
}

func cmdRemind(store *Store, now time.Time, text, when string) (string, error) {
	due, err := parseWhen(now, when)
	if err != nil {
		return "", err
	}
	r := &Reminder{ID: CommitmentID("reminder", text+due.Format(time.RFC3339)), Text: text, Due: due, State: ReminderOpen}
	store.Reminders = append(store.Reminders, r)
	return fmt.Sprintf("reminder %s set for %s", r.ID, due.Local().Format("Mon Jan 2 15:04")), nil
}

func parseDuration(raw string) (time.Duration, error) {
	if strings.HasSuffix(raw, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("bad duration %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("bad duration %q (want 3d, 12h, ...)", raw)
	}
	return d, nil
}

func parseWhen(now time.Time, raw string) (time.Time, error) {
	atFive := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, time.Local)
	}
	switch raw {
	case "today":
		return atFive(now), nil
	case "tomorrow":
		return atFive(now.AddDate(0, 0, 1)), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return atFive(t), nil
	}
	if d, err := parseDuration(raw); err == nil {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("bad time %q (want YYYY-MM-DD, today, tomorrow, or 3d)", raw)
}
