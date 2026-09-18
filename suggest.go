package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const suggestMax = 8

// Addresses that are machines, however often they write to you.
var automatedMarkers = []string{
	"noreply", "no-reply", "no_reply", "donotreply", "do-not-reply", "mailer-daemon",
	"notifications@", "notification@", "bounce", "@reply.", "postmaster", "unsubscribe",
	"@e.", "@mail.", "@email.", "@updates.", "@alerts.", "@info.", "@news.",
}

type candidate struct {
	Email    string
	Name     string
	Threads  int
	Replied  int // threads where you actually wrote back: the strongest signal
	LastSeen time.Time
}

// Role addresses belong to a company, not a person. They still count when you
// have written back — a landlord's billing address is worth keeping warm.
var rolePrefixes = []string{
	"service", "shop", "support", "sales", "billing", "info", "hello", "team", "admin",
	"contact", "orders", "order", "receipts", "invoices", "invoice", "help", "care",
	"news", "newsletter", "marketing", "alerts", "updates", "mail", "email", "accounts",
}

func automated(email string) bool {
	email = strings.ToLower(email)
	for _, marker := range automatedMarkers {
		if strings.Contains(email, marker) {
			return true
		}
	}
	return false
}

func roleAddress(email string) bool {
	local, _, found := strings.Cut(strings.ToLower(email), "@")
	if !found {
		return true
	}
	for _, prefix := range rolePrefixes {
		if local == prefix || strings.HasPrefix(local, prefix+"-") || strings.HasPrefix(local, prefix+".") {
			return true
		}
	}
	return false
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Ranks the people you actually correspond with, so tracking is a choice from a
// short list rather than something you have to remember unaided.
func suggestContacts(store *Store, cfg *Config) []candidate {
	me := strings.ToLower(cfg.MyEmail)
	byEmail := map[string]*candidate{}
	for _, t := range store.Threads {
		if t.State == ThreadNoise {
			continue
		}
		replied := !t.LastOutbound.IsZero()
		for _, participant := range t.Participants {
			email := strings.ToLower(strings.TrimSpace(participant))
			if email == "" || email == me || automated(email) {
				continue
			}
			c := byEmail[email]
			if c == nil {
				c = &candidate{Email: email}
				byEmail[email] = c
			}
			c.Threads++
			if replied {
				c.Replied++
			}
			if activity := latestActivity(t); activity.After(c.LastSeen) {
				c.LastSeen = activity
			}
		}
	}

	var out []candidate
	for email, c := range byEmail {
		if existing := store.ContactByEmail(email); existing != nil {
			if existing.Tracked {
				continue
			}
			c.Name = existing.Name
		}
		// A reply is proof of a relationship. Without one, only a personal address
		// with repeat contact qualifies — otherwise every shop you buy from lands here.
		if c.Replied == 0 && (roleAddress(email) || c.Threads < 2) {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Replied != out[j].Replied {
			return out[i].Replied > out[j].Replied
		}
		if out[i].Threads != out[j].Threads {
			return out[i].Threads > out[j].Threads
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	if len(out) > suggestMax {
		out = out[:suggestMax]
	}
	return out
}

func cmdSuggest(store *Store, cfg *Config, now time.Time) (string, []map[string]any) {
	candidates := suggestContacts(store, cfg)
	if len(candidates) == 0 {
		return "no candidates yet — tether needs a few threads you have replied to", nil
	}
	var b strings.Builder
	b.WriteString("**people worth tracking**\n")
	for _, c := range candidates {
		name := c.Name
		if name == "" {
			name = c.Email
		}
		quiet := int(now.Sub(c.LastSeen).Hours() / 24)
		fmt.Fprintf(&b, "- %s — %s, %d replied, %dd quiet\n", clean(name), plural(c.Threads, "thread"), c.Replied, quiet)
	}
	b.WriteString("\ntap to track at 60d, or `/track <email> <days>` for a different cadence")

	var buttons []map[string]any
	for _, c := range candidates {
		if len(buttons) == 5 { // Discord allows five buttons per row
			break
		}
		id := "track:" + c.Email + ":60"
		if len(id) > 100 {
			continue
		}
		label := c.Name
		if label == "" {
			label = strings.SplitN(c.Email, "@", 2)[0]
		}
		buttons = append(buttons, Button(truncate(clean(label), 20), id))
	}
	return b.String(), buttons
}
