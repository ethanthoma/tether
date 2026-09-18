package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	triageMaxPerRun    = 20
	triageMaxAttempts  = 3
	triageMsgChars     = 4000
	triageMsgsInPrompt = 2
	// Generous because reasoning tokens are billed against the same budget as the answer.
	triageMaxTokens = 2500
)

const triageSystemPrompt = `Reasoning strength: low
You classify email threads for a single user. Answer with ONE JSON object, nothing else:
{"state": "needs_reply" | "waiting_on_them" | "fyi" | "noise",
 "note": "one short sentence naming who owes what response or action, or why no response is needed",
 "commitments": [{"text": "what the user promised to do", "due": "YYYY-MM-DD or empty string"}]}
States: needs_reply = the user owes a HUMAN a reply, in prose, to something a person actually asked;
waiting_on_them = a HUMAN still owes a response or action to an explicit request from the user;
fyi = worth seeing, no outstanding response or action;
noise = newsletters, promotions, automated mail, never worth a nudge.
Sending a message does not by itself mean waiting_on_them. Closing thanks, acknowledgments,
and completed exchanges are fyi unless a specific request remains unanswered.
Use the latest messages to decide what remains outstanding; do not revive requests already resolved.
Calendar mail ("Invitation:", "Updated invitation:", "Cancelled event", accepted/declined notices)
is fyi, never needs_reply, even when the user organised it — the calendar already tracks it.
Automated mail is never needs_reply unless it states a deadline the user must personally act on.
Commitments: ONLY explicit promises made in messages sent BY the user ("I'll send X by Friday"). Usually [].`

type triageVerdict struct {
	State       ThreadState `json:"state"`
	Note        string      `json:"note"`
	Commitments []struct {
		Text string `json:"text"`
		Due  string `json:"due"`
	} `json:"commitments"`
}

func RunTriage(store *Store, cfg *Config, now time.Time) error {
	triaged := 0
	for _, thread := range store.Threads {
		if thread.State != ThreadNew || triaged >= triageMaxPerRun {
			continue
		}
		verdict, err := triageThread(store, cfg, thread, now)
		triaged++
		if err != nil {
			if errors.Is(err, errLLMUnavailable) {
				log.Printf("triage: paused, queued threads unchanged: %v", err)
				return nil
			}
			thread.TriageAttempts++
			log.Printf("triage: thread %s attempt %d: %v", thread.ShortID(), thread.TriageAttempts, err)
			if thread.TriageAttempts >= triageMaxAttempts {
				thread.State = ThreadFYI
				thread.TriageNote = "triage failed repeatedly, parked as fyi"
			}
		} else {
			thread.State = verdict.State
			thread.TriageNote = verdict.Note
			thread.TriageAttempts = 0
			applyCommitments(store, thread, verdict)
		}
		if err := store.Save(); err != nil {
			return err
		}
	}
	return nil
}

func triageThread(store *Store, cfg *Config, thread *Thread, now time.Time) (*triageVerdict, error) {
	prompt := triagePrompt(store, cfg, thread, now)
	for attempt := 0; attempt < 2; attempt++ {
		content, err := LLMChat(cfg, triageSystemPrompt, prompt, 0, triageMaxTokens)
		if err != nil {
			return nil, err
		}
		verdict, err := parseVerdict(content)
		if err == nil {
			return verdict, nil
		}
		if attempt == 1 {
			return nil, err
		}
	}
	panic("unreachable")
}

func parseVerdict(content string) (*triageVerdict, error) {
	raw, err := ExtractJSON(content)
	if err != nil {
		return nil, err
	}
	var verdict triageVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return nil, fmt.Errorf("triage: bad verdict JSON: %w", err)
	}
	switch verdict.State {
	case ThreadNeedsReply, ThreadWaitingOnThem, ThreadFYI, ThreadNoise:
		return &verdict, nil
	}
	return nil, fmt.Errorf("triage: invalid verdict state %q", verdict.State)
}

func applyCommitments(store *Store, thread *Thread, verdict *triageVerdict) {
	for _, c := range verdict.Commitments {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		id := CommitmentID(thread.ID, text)
		exists := false
		for _, existing := range store.Commitments {
			if existing.ID == id {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		commitment := &Commitment{ID: id, Text: text, ThreadID: thread.ID, State: CommitmentOpen}
		if due, err := time.ParseInLocation("2006-01-02", c.Due, time.Local); err == nil {
			commitment.Due = due.Add(17 * time.Hour)
		}
		store.Commitments = append(store.Commitments, commitment)
	}
}

func triagePrompt(store *Store, cfg *Config, thread *Thread, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "User's email address: %s\nToday: %s\nSubject: %s\nParticipants: %s\n",
		cfg.MyEmail, now.Format("2006-01-02"), thread.Subject, strings.Join(thread.Participants, ", "))
	msgIDs := thread.MsgIDs
	if len(msgIDs) > triageMsgsInPrompt {
		msgIDs = msgIDs[len(msgIDs)-triageMsgsInPrompt:]
	}
	for _, msgID := range msgIDs {
		msg, err := store.LoadMessage(msgID)
		if err != nil {
			continue
		}
		sender := msg.From
		if msg.Outbound {
			sender = "THE USER"
		}
		body := msg.Body
		if len(body) > triageMsgChars {
			body = body[:triageMsgChars]
		}
		fmt.Fprintf(&b, "\n--- message from %s on %s ---\n%s\n", sender, msg.Date.Format("2006-01-02"), body)
	}
	return b.String()
}
