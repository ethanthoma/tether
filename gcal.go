package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleCalendarAPI = "https://www.googleapis.com/calendar/v3"
	// Creating a secondary calendar needs the full scope; calendar.events alone
	// can only write into calendars that already exist.
	googleScope     = "https://www.googleapis.com/auth/calendar"
	googleTimeout   = 30 * time.Second
	googleMaxBody   = 1 << 20
	tetherCalendar  = "tether"
	eventDefaultMin = 60
)

var eventReminders = []int{24 * 60, 30}

func googleAccessToken(cfg *Config) (string, error) {
	form := url.Values{
		"client_id":     {cfg.GoogleClientID},
		"client_secret": {cfg.GoogleClientSecret},
		"refresh_token": {cfg.GoogleRefreshToken},
		"grant_type":    {"refresh_token"},
	}
	client := &http.Client{Timeout: googleTimeout}
	resp, err := client.PostForm(googleTokenURL, form)
	if err != nil {
		return "", fmt.Errorf("google: token refresh: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, googleMaxBody))
	if err != nil {
		return "", fmt.Errorf("google: read token: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("google: token refresh failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("google: no access token in response")
	}
	return token.AccessToken, nil
}

func googleDo(cfg *Config, token, method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("google: marshal: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, googleCalendarAPI+path, payload)
	if err != nil {
		return nil, fmt.Errorf("google: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: googleTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, googleMaxBody))
	if err != nil {
		return nil, fmt.Errorf("google: read: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google: %s: status %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// The calendar id is cached in the store so the common path is a single insert.
func tetherCalendarID(store *Store, cfg *Config, token string) (string, error) {
	if store.Sync.CalendarID != "" {
		return store.Sync.CalendarID, nil
	}
	data, err := googleDo(cfg, token, "GET", "/users/me/calendarList?maxResults=250", nil)
	if err != nil {
		return "", err
	}
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return "", fmt.Errorf("google: decode calendar list: %w", err)
	}
	for _, item := range list.Items {
		if item.Summary == tetherCalendar {
			store.Sync.CalendarID = item.ID
			return item.ID, store.Save()
		}
	}
	created, err := googleDo(cfg, token, "POST", "/calendars", map[string]string{"summary": tetherCalendar})
	if err != nil {
		return "", err
	}
	var calendar struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &calendar); err != nil || calendar.ID == "" {
		return "", fmt.Errorf("google: created calendar has no id")
	}
	store.Sync.CalendarID = calendar.ID
	return calendar.ID, store.Save()
}

func CreateEvent(store *Store, cfg *Config, summary string, start time.Time, minutes int) (string, error) {
	token, err := googleAccessToken(cfg)
	if err != nil {
		return "", err
	}
	calendarID, err := tetherCalendarID(store, cfg, token)
	if err != nil {
		return "", err
	}
	overrides := make([]map[string]any, 0, len(eventReminders))
	for _, m := range eventReminders {
		overrides = append(overrides, map[string]any{"method": "popup", "minutes": m})
	}
	event := map[string]any{
		"summary": summary,
		"start":   map[string]string{"dateTime": start.Format(time.RFC3339)},
		"end":     map[string]string{"dateTime": start.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)},
		"reminders": map[string]any{
			"useDefault": false,
			"overrides":  overrides,
		},
	}
	data, err := googleDo(cfg, token, "POST", "/calendars/"+url.PathEscape(calendarID)+"/events", event)
	if err != nil {
		return "", err
	}
	var created struct {
		HTMLLink string `json:"htmlLink"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", fmt.Errorf("google: decode event: %w", err)
	}
	return created.HTMLLink, nil
}

func validEventTime(now time.Time, raw string) bool {
	_, err := parseEventTime(now, raw)
	return err == nil
}

// Accepts "2026-09-01 14:30", "tomorrow 3pm", "today 09:00", "3pm", "2026-09-01".
func parseEventTime(now time.Time, raw string) (time.Time, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return time.Time{}, fmt.Errorf("when is empty")
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	clock := ""
	switch fields := strings.Fields(raw); {
	case len(fields) > 2:
		return time.Time{}, fmt.Errorf("bad time %q (want \"2026-09-01 14:30\", \"tomorrow 3pm\")", raw)
	case len(fields) == 2:
		parsed, err := parseEventDay(now, day, fields[0])
		if err != nil {
			return time.Time{}, err
		}
		day, clock = parsed, fields[1]
	default:
		if parsed, err := parseEventDay(now, day, fields[0]); err == nil {
			day, clock = parsed, "09:00"
		} else {
			clock = fields[0]
		}
	}

	hour, minute, err := parseClock(clock)
	if err != nil {
		return time.Time{}, err
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.Local)
	if at.Before(now) && len(strings.Fields(raw)) == 1 && !strings.Contains(raw, "-") {
		at = at.AddDate(0, 0, 1)
	}
	return at, nil
}

func parseEventDay(now, today time.Time, raw string) (time.Time, error) {
	switch raw {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("bad date %q (want YYYY-MM-DD, today, or tomorrow)", raw)
}

func parseClock(raw string) (int, int, error) {
	raw = strings.TrimSpace(raw)
	suffix := ""
	for _, s := range []string{"am", "pm"} {
		if strings.HasSuffix(raw, s) {
			suffix = s
			raw = strings.TrimSpace(strings.TrimSuffix(raw, s))
		}
	}
	layouts := []string{"15:04", "3:04", "3", "15"}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		hour := parsed.Hour()
		if suffix == "pm" && hour < 12 {
			hour += 12
		}
		if suffix == "am" && hour == 12 {
			hour = 0
		}
		if suffix == "" && hour > 23 {
			break
		}
		return hour, parsed.Minute(), nil
	}
	return 0, 0, fmt.Errorf("bad time of day %q (want 14:30, 3pm, or 3:30pm)", raw)
}

func googleConsentURL(cfg *Config, redirect string) string {
	params := url.Values{
		"client_id":     {cfg.GoogleClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {googleScope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	return googleAuthURL + "?" + params.Encode()
}

func googleExchangeCode(cfg *Config, code, redirect string) (string, error) {
	form := url.Values{
		"client_id":     {cfg.GoogleClientID},
		"client_secret": {cfg.GoogleClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirect},
	}
	client := &http.Client{Timeout: googleTimeout}
	resp, err := client.PostForm(googleTokenURL, form)
	if err != nil {
		return "", fmt.Errorf("google: exchange code: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, googleMaxBody))
	if err != nil {
		return "", fmt.Errorf("google: read token: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("google: exchange failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var token struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &token); err != nil || token.RefreshToken == "" {
		return "", fmt.Errorf("google: no refresh_token returned (revoke the app's access and retry)")
	}
	return token.RefreshToken, nil
}
