package main

import (
	"strings"
	"testing"
	"time"
)

func TestThreadNudgeContext(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, state := range []ThreadState{ThreadNeedsReply, ThreadWaitingOnThem} {
		t.Run(string(state), func(t *testing.T) {
			thread := &Thread{ID: "<context@example.com>", State: state, Subject: "**Entry timeout**",
				Participants: []string{"alex@example.com"}, TriageNote: "Alex owes an answer about extending the entry timeout.",
				LastInbound: now.Add(-7 * 24 * time.Hour), LastOutbound: now.Add(-7 * 24 * time.Hour)}
			nudges := collectNudges(&Store{Threads: []*Thread{thread}}, &nudgeLog{}, now, nil)
			if len(nudges) != 1 {
				t.Fatalf("want one reminder, got %+v", nudges)
			}
			for _, fragment := range []string{"Entry timeout", "With: alex@example.com", "Context: " + thread.TriageNote} {
				if !strings.Contains(nudges[0].Body, fragment) {
					t.Errorf("missing %q in %q", fragment, nudges[0].Body)
				}
			}
			if strings.Contains(nudges[0].Body, "**") {
				t.Error("subject markdown was not cleaned")
			}
			if nudges[0].Target != thread.ShortID() || !nudges[0].Thread {
				t.Error("thread action target changed")
			}
		})
	}
}

func TestThreadNudgeContextBounds(t *testing.T) {
	thread := &Thread{ID: "<context@example.com>", Subject: "Shipment"}
	if body := threadNudgeBody(thread, "quiet for 6d"); body != "["+thread.ShortID()+"] Shipment — quiet for 6d" {
		t.Fatalf("missing context should retain the basic reminder: %q", body)
	}
	thread.Subject = strings.Repeat("界", 500)
	thread.TriageNote = strings.Repeat("界", 1000)
	thread.Participants = []string{"a@example.com", "b@example.com", "c@example.com", "omitted@example.com"}
	body := threadNudgeBody(thread, "quiet for 6d")
	if len([]rune(body)) > 900 || strings.Contains(body, "omitted@example.com") {
		t.Fatal("reminder context exceeded its bounds")
	}
}

func fireAll(t *testing.T, nl *nudgeLog, nudges []Nudge, at time.Time) {
	t.Helper()
	for _, n := range nudges {
		n.FiredAt = at
		if err := nl.append(n); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNeedsReplyRule(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.Local)
	s.Threads = append(s.Threads, &Thread{
		ID: "<a@x>", State: ThreadNeedsReply, Subject: "invoice", LastInbound: now.Add(-3 * 24 * time.Hour),
	})
	nl, err := openNudgeLog(s.dir)
	if err != nil {
		t.Fatal(err)
	}

	first := collectNudges(s, nl, now, nil)
	if len(first) != 1 || first[0].RuleID != "needs_reply" {
		t.Fatalf("expected one needs_reply nudge, got %+v", first)
	}
	fireAll(t, nl, first, now)

	if again := collectNudges(s, nl, now.Add(time.Hour), nil); len(again) != 0 {
		t.Fatalf("cooldown violated: %+v", again)
	}
	second := collectNudges(s, nl, now.Add(80*time.Hour), nil)
	if len(second) != 1 {
		t.Fatalf("expected second fire after cooldown, got %+v", second)
	}
	fireAll(t, nl, second, now.Add(80*time.Hour))
	if third := collectNudges(s, nl, now.Add(200*time.Hour), nil); len(third) != 0 {
		t.Fatalf("max 2 fires violated: %+v", third)
	}
}

func TestSnoozeSuppressesNudges(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	s.Threads = append(s.Threads, &Thread{
		ID: "<a@x>", State: ThreadNeedsReply, LastInbound: now.Add(-72 * time.Hour),
		SnoozeUntil: now.Add(24 * time.Hour),
	})
	nl, _ := openNudgeLog(s.dir)
	if nudges := collectNudges(s, nl, now, nil); len(nudges) != 0 {
		t.Fatalf("snoozed thread must not nudge: %+v", nudges)
	}
}

func TestCommitmentAndReminderRules(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	s.Commitments = append(s.Commitments,
		&Commitment{ID: "soon", Text: "send report", State: CommitmentOpen, Due: now.Add(12 * time.Hour)},
		&Commitment{ID: "late", Text: "review PR", State: CommitmentSlipped, Due: now.Add(-48 * time.Hour)},
		&Commitment{ID: "far", Text: "plan trip", State: CommitmentOpen, Due: now.Add(10 * 24 * time.Hour)},
	)
	s.Reminders = append(s.Reminders,
		&Reminder{ID: "r1", Text: "water plants", State: ReminderOpen, Due: now.Add(-time.Hour)},
		&Reminder{ID: "r2", Text: "future", State: ReminderOpen, Due: now.Add(48 * time.Hour)},
	)
	nl, _ := openNudgeLog(s.dir)
	got := map[string]bool{}
	for _, n := range collectNudges(s, nl, now, nil) {
		got[n.RuleID+"/"+n.EntityID] = true
	}
	for _, want := range []string{"due_soon/soon", "slipped/late", "reminder/r1"} {
		if !got[want] {
			t.Fatalf("missing nudge %s in %v", want, got)
		}
	}
	if got["due_soon/far"] || got["reminder/r2"] {
		t.Fatalf("future items must not nudge: %v", got)
	}
}

func TestStaleContactDailyCap(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.Local)
	old := now.Add(-100 * 24 * time.Hour)
	s.Contacts = append(s.Contacts,
		&Contact{Email: "a@x.com", Tracked: true, CadenceDays: 30, LastContactAt: old},
		&Contact{Email: "b@x.com", Tracked: true, CadenceDays: 30, LastContactAt: old},
		&Contact{Email: "c@x.com", Tracked: false, LastContactAt: old},
	)
	nl, _ := openNudgeLog(s.dir)
	stale := 0
	for _, n := range collectNudges(s, nl, now, nil) {
		if n.RuleID == "stale_contact" {
			stale++
		}
	}
	if stale != 1 {
		t.Fatalf("max 1 staleness nudge per day, got %d", stale)
	}
}

func TestEventPrepRule(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	s.Threads = append(s.Threads, &Thread{
		ID: "<a@x>", State: ThreadNeedsReply, Subject: "agenda", LastInbound: now.Add(-time.Hour),
		Participants: []string{"bob@example.com"},
	})
	s.Events = append(s.Events, Event{
		UID: "e1", Summary: "1:1 with Bob", Start: now.Add(6 * time.Hour), Attendees: []string{"bob@example.com"},
	})
	nl, _ := openNudgeLog(s.dir)
	found := false
	for _, n := range collectNudges(s, nl, now, nil) {
		if n.RuleID == "event_prep" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected event_prep nudge for meeting with owed reply")
	}
}

func TestQuietHours(t *testing.T) {
	late := time.Date(2026, 8, 17, 23, 30, 0, 0, time.Local)
	early := time.Date(2026, 8, 17, 7, 59, 0, 0, time.Local)
	midday := time.Date(2026, 8, 17, 12, 0, 0, 0, time.Local)
	if !inQuietHours(late) || !inQuietHours(early) {
		t.Fatal("22:00-08:00 must be quiet")
	}
	if inQuietHours(midday) {
		t.Fatal("midday must not be quiet")
	}
}
