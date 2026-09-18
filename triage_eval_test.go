//go:build triage_eval

package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestTriageEmailExamples(t *testing.T) {
	if os.Getenv("TETHER_LLM_URL") == "" {
		t.Fatal("set TETHER_LLM_URL and, if required, LLAMA_API_KEY to run the live evaluation")
	}
	cfg := loadConfig()
	cfg.MyEmail = "user@example.com"
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, example := range []struct {
		name     string
		subject  string
		messages []Message
		state    ThreadState
	}{
		{"resolved_shipping", "Shipment status", []Message{
			{Body: "Your certificate shipped last week. Use the tracking number to check its status."},
			{Outbound: true, Body: "That's great, thank you very much!\n\nOn Monday, Support wrote:\n> Your certificate shipped last week.\n> Earlier you asked: Where is my shipment?"},
		}, ThreadFYI},
		{"calendar_invitation", "Invitation: furniture delivery", []Message{
			{Body: "Furniture delivery, Friday 3pm. Your attendance is optional. Reply to this invitation in Google Calendar. You are receiving Calendar notifications."},
		}, ThreadFYI},
		{"optional_permissions", "App requesting updated permissions", []Message{
			{Body: "An installed app requests updated permissions. Visit your account settings to accept or ignore this request. If ignored, it retains its current permissions."},
		}, ThreadFYI},
		{"thanks_only", "Thanks and introductions", []Message{
			{Outbound: true, Body: "Hello Alex, thank you for your time yesterday, it was very helpful! My interests are small models and iterative computation. Best, User"},
		}, ThreadFYI},
		{"introduction_request", "Thanks and introductions", []Message{
			{Outbound: true, Body: "Thank you for your time yesterday! An introduction to Morgan would mean a lot. Please let me know if you need anything else from me."},
		}, ThreadWaitingOnThem},
		{"building_request", "Entry system timeout", []Message{
			{Outbound: true, Body: "The elevator is broken, so visitors wait long enough for the entry system to time out. Could you extend the timeout until it is repaired? Thank you."},
		}, ThreadWaitingOnThem},
		{"human_reply_needed", "Entry system timeout", []Message{
			{Body: "Could you tell me which entrance times out and roughly how long visitors wait? I need those details to adjust the system."},
		}, ThreadNeedsReply},
	} {
		t.Run(example.name, func(t *testing.T) {
			store := testStore(t)
			thread := &Thread{ID: "<root@example.com>", Subject: example.subject, Participants: []string{"alex@example.com"}}
			for index, message := range example.messages {
				message.MsgID = fmt.Sprintf("<message-%d@example.com>", index)
				message.From = "alex@example.com"
				message.Date = now.Add(time.Duration(index-len(example.messages)) * time.Hour)
				if message.Outbound {
					message.From = cfg.MyEmail
				}
				if err := store.SaveMessage(&message); err != nil {
					t.Fatal(err)
				}
				thread.MsgIDs = append(thread.MsgIDs, message.MsgID)
			}
			verdict, err := triageThread(store, cfg, thread, now)
			if err != nil {
				t.Fatal(err)
			}
			if verdict.State != example.state {
				t.Errorf("got %s, want %s: %s", verdict.State, example.state, verdict.Note)
			}
			if len(verdict.Commitments) != 0 {
				t.Errorf("invented commitments: %+v", verdict.Commitments)
			}
			t.Logf("%s: %s", verdict.State, verdict.Note)
		})
	}
}
