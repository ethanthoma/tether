package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

const digestSystemPrompt = `Reasoning strength: low
You write the 2-3 sentence opening of a personal morning briefing addressed to %s, who is
reading it about their OWN mailbox. Address them as "you" and never by name; a name in a
subject line is the other party or the calendar organiser, not a third person to contact.
Right now it is %s — never suggest acting before a time that has already passed.
Plain, direct, lowercase-leaning, no greetings, no emoji. Point at the one or two things
that matter most. Output ONLY 2-3 plain prose sentences: no headings, no bullet points,
no restating the list, no questions, no offers of help.`

const introMaxChars = 400

func RunDigest(store *Store, cfg *Config, now time.Time) error {
	body := buildDigest(store, now)
	system := fmt.Sprintf(digestSystemPrompt, cfg.MyEmail, now.Format("Mon Jan 2 15:04"))
	raw, err := LLMChat(cfg, system, body, 0.2, 4000)
	if err != nil {
		log.Printf("digest: intro skipped: %v", err)
	} else {
		body = composeDigest(body, introProse(raw))
	}
	posted, err := PostDiscordMessage(cfg, body)
	if err != nil {
		return err
	}
	// A thread under the digest is where follow-up questions live.
	thread, err := OpenThread(cfg, posted, "digest "+now.Local().Format("Mon Jan 2"))
	if err != nil {
		log.Printf("digest: thread not opened: %v", err)
		return nil
	}
	store.Sync.ChatThread = thread
	store.Sync.ChatThreadAt = now
	return store.Save()
}

// The header stays the first line: the intro reads as a lede under it, not as a
// paragraph arriving before you know what you are looking at.
func composeDigest(body, intro string) string {
	if intro == "" {
		return body
	}
	header, sections, found := strings.Cut(body, "\n")
	if !found {
		return body + "\n\n" + intro
	}
	return header + "\n\n" + intro + "\n" + sections
}

// The intro is the one part of the digest a model writes, and it strays: it
// reformats the list, adds emoji, asks questions. Keep only plain prose.
func introProse(raw string) string {
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "-"), strings.HasPrefix(line, "*"), strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, ">"), strings.HasPrefix(line, "|"):
			continue
		case strings.HasSuffix(line, "?"):
			continue
		}
		kept = append(kept, line)
		if len(kept) == 3 {
			break
		}
	}
	return truncate(clean(strings.Join(kept, " ")), introMaxChars)
}

func buildDigest(store *Store, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**tether digest — %s**\n", now.Local().Format("Mon Jan 2"))

	var needsReply, waiting []*Thread
	for _, t := range store.Threads {
		if t.Snoozed(now) {
			continue
		}
		switch t.State {
		case ThreadNeedsReply:
			needsReply = append(needsReply, t)
		case ThreadWaitingOnThem:
			if now.Sub(t.LastOutbound) > 3*24*time.Hour {
				waiting = append(waiting, t)
			}
		}
	}
	sort.Slice(needsReply, func(i, j int) bool { return needsReply[i].LastInbound.Before(needsReply[j].LastInbound) })
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].LastOutbound.Before(waiting[j].LastOutbound) })

	section(&b, "needs your reply", threadLines(needsReply, now, "in"))
	section(&b, "waiting on them", threadLines(waiting, now, "out"))

	var open, slipped []string
	sorted := append([]*Commitment(nil), store.Commitments...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Due.IsZero() != sorted[j].Due.IsZero() {
			return !sorted[i].Due.IsZero()
		}
		return sorted[i].Due.Before(sorted[j].Due)
	})
	for _, c := range sorted {
		line := c.Text
		if !c.Due.IsZero() {
			line += " (due " + c.Due.Local().Format("Mon Jan 2") + ")"
		}
		switch c.State {
		case CommitmentOpen:
			open = append(open, line)
		case CommitmentSlipped:
			slipped = append(slipped, line)
		}
	}
	section(&b, "commitments", open)
	section(&b, "slipped", slipped)

	var reminders []string
	for _, r := range store.Reminders {
		if r.State == ReminderOpen && r.Due.Before(now.Add(7*24*time.Hour)) {
			reminders = append(reminders, fmt.Sprintf("%s (%s)", r.Text, r.Due.Local().Format("Mon Jan 2")))
		}
	}
	section(&b, "reminders", reminders)

	var events []string
	dayEnd := now.Add(48 * time.Hour)
	for _, e := range store.Events {
		if e.Start.After(now.Add(-time.Hour)) && e.Start.Before(dayEnd) {
			when := e.Start.Local().Format("Mon 15:04")
			if e.AllDay {
				when = e.Start.Local().Format("Mon") + " all day"
			}
			events = append(events, fmt.Sprintf("%s — %s", when, e.Summary))
		}
	}
	section(&b, "next 48h", events)

	var stale []string
	for _, c := range store.Contacts {
		if c.Tracked && c.CadenceDays > 0 && now.Sub(c.LastContactAt) > time.Duration(c.CadenceDays)*24*time.Hour {
			name := c.Name
			if name == "" {
				name = c.Email
			}
			stale = append(stale, fmt.Sprintf("%s — %dd quiet", name, int(now.Sub(c.LastContactAt).Hours()/24)))
		}
	}
	section(&b, "losing touch", stale)

	if b.Len() < 60 {
		b.WriteString("\nall clear.\n")
	}
	return b.String()
}

func threadLines(threads []*Thread, now time.Time, direction string) []string {
	var lines []string
	for _, t := range threads {
		last := t.LastInbound
		if direction == "out" {
			last = t.LastOutbound
		}
		days := int(now.Sub(last).Hours() / 24)
		lines = append(lines, fmt.Sprintf("`%s` %s — %dd", t.ShortID(), clean(t.Subject), days))
	}
	return lines
}

func section(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s**\n", title)
	for _, line := range lines {
		fmt.Fprintf(b, "- %s\n", line)
	}
}
