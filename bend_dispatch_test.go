//go:build bend

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBendDispatchAgreement(t *testing.T) {
	originalTransport := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var program strings.Builder
	program.WriteString(`import Base
import ./dispatch.bend as Model

def main() -> IO(Unit):
  do IO<Unit>:
`)
	var expected []string
	for _, clock := range []struct {
		hour   int
		minute int
		second int
		quiet  bool
	}{
		{0, 0, 0, true}, {7, 59, 59, true}, {8, 0, 0, false},
		{12, 0, 0, false}, {21, 59, 59, false}, {22, 0, 0, true}, {23, 59, 59, true},
	} {
		for _, fired := range []int{0, 1, 4, 5, 6} {
			for _, candidates := range []int{0, 1, 4, 5, 8} {
				name := fmt.Sprintf("%02d:%02d:%02d/%d/%d", clock.hour, clock.minute, clock.second, fired, candidates)
				t.Run(name, func(t *testing.T) {
					store := testStore(t)
					if err := os.WriteFile(filepath.Join(store.dir, "bend-eligibility.enabled"), nil, 0600); err != nil {
						t.Fatal(err)
					}
					if os.Getenv("TETHER_BEND_SHADOW") == "" {
						t.Fatal("native evaluator required")
					}
					now := time.Date(2026, 9, 17, clock.hour, clock.minute, clock.second, 0, time.Local)
					midnight := time.Date(2026, 9, 17, 0, 0, 0, 0, time.Local)
					log, err := openNudgeLog(store.dir)
					if err != nil {
						t.Fatal(err)
					}
					for index := 0; index < fired; index++ {
						if err := log.append(Nudge{RuleID: "prior", EntityID: fmt.Sprint(index), FiredAt: midnight}); err != nil {
							t.Fatal(err)
						}
					}
					for index := 0; index < maxPushesPerDay; index++ {
						if err := log.append(Nudge{RuleID: "prior", EntityID: "yesterday", FiredAt: midnight.Add(-time.Nanosecond)}); err != nil {
							t.Fatal(err)
						}
					}
					for index := 0; index < candidates; index++ {
						store.Reminders = append(store.Reminders, &Reminder{ID: fmt.Sprintf("r%d", index),
							State: ReminderOpen, Text: "Test reminder", Due: now.Add(-time.Hour)})
					}
					*transport = bendDiscordTransport{}
					if err := RunNudges(store, &Config{DiscordChannel: "test", BendShadow: os.Getenv("TETHER_BEND_SHADOW")}, now); err != nil {
						t.Fatal(err)
					}
					remaining := max(0, maxPushesPerDay-fired)
					if transport.sent > remaining || transport.sent > candidates {
						t.Fatalf("sent %d, allowance %d, candidates %d", transport.sent, remaining, candidates)
					}
					if clock.quiet && transport.sent != 0 {
						t.Fatal("sent during quiet hours")
					}
					persisted, err := openNudgeLog(store.dir)
					if err != nil {
						t.Fatal(err)
					}
					if count := persisted.firedToday("", now); count != fired+transport.sent {
						t.Fatalf("persisted today count %d, want %d", count, fired+transport.sent)
					}
					expected = append(expected, fmt.Sprintf("case:%s=%d", name, transport.sent))
					quiet := "False{}"
					if clock.quiet {
						quiet = "True{}"
					}
					fmt.Fprintf(&program, "    IO.print(%q ++ Nat.show(Model.batch_count(%s, %dn, %dn)))\n",
						"case:"+name+"=", quiet, fired, candidates)
				})
			}
		}
	}
	bendCompare(t, "dispatch.bend", program.String(), expected)
	t.Logf("Go and Bend agree on %d dispatch cases", len(expected))
}

func TestBendDeliveryAgreement(t *testing.T) {
	originalTransport := http.DefaultTransport
	transport := &bendDiscordTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var program strings.Builder
	program.WriteString(`import Base
import ./dispatch.bend as Model

def show_result(state: Model.DeliveryState) -> String:
  match state:
    case Model.Ready{+confirmed}:
      Nat.show(confirmed) ++ "/false/" ++ Nat.show(confirmed)
    case Model.Failed{+confirmed}:
      Nat.show(confirmed) ++ "/true/" ++ Nat.show(1n+confirmed)

def main() -> IO(Unit):
  do IO<Unit>:
`)
	var expected []string
	for _, quiet := range []bool{false, true} {
		for _, fired := range []int{0, 4, 5} {
			for _, candidates := range []int{0, 1, 5, 8} {
				for _, successes := range []int{0, 1, 4} {
					for _, status := range []int{0, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
						name := fmt.Sprintf("%t/%d/%d/%d/%d", quiet, fired, candidates, successes, status)
						t.Run(name, func(t *testing.T) {
							store := testStore(t)
							if err := os.WriteFile(filepath.Join(store.dir, "bend-eligibility.enabled"), nil, 0600); err != nil {
								t.Fatal(err)
							}
							if os.Getenv("TETHER_BEND_SHADOW") == "" {
								t.Fatal("native evaluator required")
							}
							hour := 12
							bendQuiet := "False{}"
							if quiet {
								hour = 23
								bendQuiet = "True{}"
							}
							now := time.Date(2026, 9, 17, hour, 0, 0, 0, time.Local)
							log, err := openNudgeLog(store.dir)
							if err != nil {
								t.Fatal(err)
							}
							for index := 0; index < fired; index++ {
								if err := log.append(Nudge{RuleID: "prior", EntityID: fmt.Sprint(index), FiredAt: now}); err != nil {
									t.Fatal(err)
								}
							}
							for index := 0; index < candidates; index++ {
								store.Reminders = append(store.Reminders, &Reminder{ID: fmt.Sprintf("r%d", index),
									State: ReminderOpen, Text: "Test reminder", Due: now.Add(-time.Hour)})
							}
							*transport = bendDiscordTransport{failureAt: successes + 1, status: status}
							cfg := &Config{DiscordChannel: "test", BendShadow: os.Getenv("TETHER_BEND_SHADOW")}
							err = RunNudges(store, cfg, now)
							if err != nil && !strings.Contains(err.Error(), "simulated rejection") {
								t.Fatal(err)
							}
							firstSent := transport.sent
							if firstSent > candidates || firstSent > max(0, maxPushesPerDay-fired) {
								t.Fatalf("confirmed count exceeds available candidates or budget: %d", firstSent)
							}
							expected = append(expected, fmt.Sprintf("case:%s/first=%d/%t/%d", name, firstSent, err != nil, transport.attempts))
							fmt.Fprintf(&program, "    IO.print(%q ++ show_result(Model.deliver(%s, %dn, %dn, %dn)))\n",
								"case:"+name+"/first=", bendQuiet, fired, candidates, successes)
							persisted, err := openNudgeLog(store.dir)
							if err != nil {
								t.Fatal(err)
							}
							if len(persisted.records) != fired+firstSent {
								t.Fatalf("failed requests affected the persisted count: %+v", persisted.records)
							}
							*transport = bendDiscordTransport{}
							if err := RunNudges(store, cfg, now); err != nil {
								t.Fatal(err)
							}
							expected = append(expected, fmt.Sprintf("case:%s/retry=%d/false/%d", name, transport.sent, transport.attempts))
							fmt.Fprintf(&program, "    IO.print(%q ++ show_result(Model.deliver(%s, %dn, %dn, 8n)))\n",
								"case:"+name+"/retry=", bendQuiet, fired+firstSent, candidates-firstSent)
							persisted, err = openNudgeLog(store.dir)
							if err != nil {
								t.Fatal(err)
							}
							if len(persisted.records) != fired+firstSent+transport.sent {
								t.Fatal("retry count does not match confirmed deliveries")
							}
							for index, record := range persisted.records[fired:] {
								if record.EntityID != fmt.Sprintf("r%d", index) {
									t.Fatalf("retry skipped or repeated a reminder: %+v", persisted.records)
								}
							}
						})
					}
				}
			}
		}
	}
	bendCompare(t, "dispatch.bend", program.String(), expected)
	t.Logf("Go and Bend agree on %d delivery and retry outcomes", len(expected))
}
