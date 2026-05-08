package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := &Notifier{
		BotToken: "testtoken",
		ChatID:   "12345",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Swap the real Telegram URL for the test server by temporarily overriding Send logic
	// via a thin wrapper that uses the test server URL.
	ctx := context.Background()
	err := sendTo(ctx, n, srv.URL+"/bot%s/sendMessage", "hello *world*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["chat_id"] != "12345" {
		t.Errorf("chat_id: got %q, want %q", gotBody["chat_id"], "12345")
	}
	if gotBody["text"] != "hello *world*" {
		t.Errorf("text: got %q, want %q", gotBody["text"], "hello *world*")
	}
	if gotBody["parse_mode"] != "Markdown" {
		t.Errorf("parse_mode: got %q, want %q", gotBody["parse_mode"], "Markdown")
	}
}

func TestSendNoOp(t *testing.T) {
	// Neither env var is set — Send must be a no-op with no error.
	os.Unsetenv("TELEGRAM_BOT_TOKEN")
	os.Unsetenv("TELEGRAM_CHAT_ID")

	n := FromEnv()
	if err := n.Send(context.Background(), "should not send"); err != nil {
		t.Fatalf("expected no error from no-op notifier, got: %v", err)
	}
}

func TestSendDisabledWhenPartialEnv(t *testing.T) {
	os.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	os.Unsetenv("TELEGRAM_CHAT_ID")
	defer os.Unsetenv("TELEGRAM_BOT_TOKEN")

	n := FromEnv()
	if err := n.Send(context.Background(), "should not send"); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
}

// ── SendStructured: per-tier semantics ────────────────────────────────────────
//
// Tests cover the locked design (results/telegram_alert_design_decision_rule_2026-05-08.md):
//   - Tier prefix in message
//   - Rate limiting: INFO ≤10/hour, WARN ≤5/hour, CRITICAL unlimited
//   - Aggregation footer when suppressed events drain
//   - Mute window: INFO fully suppressed, WARN [QUIET]-tagged, CRITICAL always
//   - parseMuteHours valid + invalid inputs
//   - Cross-midnight mute window
//   - rateLimiter goroutine-safe under concurrent access

// startCaptureServer returns a test server + a captured-bodies pointer + cleanup.
// The bodies slice is mutex-protected because HandlerFunc runs in a goroutine
// per request and the goroutine-safety test fires 100 concurrent requests.
func startCaptureServer() (urlFmt string, bodies *[]string, cleanup func()) {
	var mu sync.Mutex
	captured := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msg map[string]string
		json.Unmarshal(b, &msg)
		mu.Lock()
		captured = append(captured, msg["text"])
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	return srv.URL + "/bot%s/sendMessage", &captured, srv.Close
}

func newTestNotifier() *Notifier {
	return &Notifier{
		BotToken:    "tok",
		ChatID:      "chat",
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		rateLimiter: newRateLimiter(),
	}
}

func TestSendStructured_NoOpWhenNotConfigured(t *testing.T) {
	n := &Notifier{rateLimiter: newRateLimiter()} // no token/chat
	err := n.SendStructured(context.Background(), SeverityInfo, "test")
	if err != nil {
		t.Errorf("no-op SendStructured returned error: %v", err)
	}
}

func TestSendStructured_FormatPrefix_AllTiers(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) // not in any mute window

	cases := []struct {
		severity Severity
		body     string
		wantPfx  string
	}{
		{SeverityInfo, "info body", "ℹ"},
		{SeverityWarn, "warn body", "⚠"},
		{SeverityCritical, "crit body", "🚨"},
	}
	for _, c := range cases {
		if err := n.sendStructuredAt(context.Background(), urlFmt, c.severity, c.body, now); err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}

	if len(*bodies) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(*bodies))
	}
	for i, c := range cases {
		want := c.wantPfx + " " + c.body
		if (*bodies)[i] != want {
			t.Errorf("message %d: got %q, want %q", i, (*bodies)[i], want)
		}
	}
}

func TestSendStructured_RateLimit_Info_10PerHour(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	// 10 INFO sends: all should pass.
	for i := 0; i < 10; i++ {
		_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityInfo, fmt.Sprintf("msg %d", i), now.Add(time.Duration(i)*time.Second))
	}
	if len(*bodies) != 10 {
		t.Fatalf("first 10: expected 10 sent, got %d", len(*bodies))
	}

	// 11th INFO within the same hour: blocked.
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityInfo, "msg 11", now.Add(11*time.Second))
	if len(*bodies) != 10 {
		t.Errorf("11th INFO send: got %d total, want 10 (rate-limited)", len(*bodies))
	}
}

func TestSendStructured_RateLimit_Warn_5PerHour(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 7; i++ {
		_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, fmt.Sprintf("warn %d", i), now.Add(time.Duration(i)*time.Second))
	}
	// 5 sent, 2 suppressed.
	if len(*bodies) != 5 {
		t.Errorf("WARN rate limit: got %d sent, want 5", len(*bodies))
	}
}

func TestSendStructured_RateLimit_Critical_Unlimited(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	// 50 CRITICAL: all should pass.
	for i := 0; i < 50; i++ {
		_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, fmt.Sprintf("crit %d", i), now.Add(time.Duration(i)*time.Second))
	}
	if len(*bodies) != 50 {
		t.Errorf("CRITICAL unlimited: got %d sent, want 50", len(*bodies))
	}
}

func TestSendStructured_AggregationFooter_AfterSuppression(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	// Send 5 WARNs (cap), then 2 more (suppressed), then jump 1.5h forward and send 1 more.
	// The 8th send should drain the suppression count and include the aggregation footer.
	for i := 0; i < 7; i++ {
		_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, fmt.Sprintf("w %d", i), now.Add(time.Duration(i)*time.Second))
	}
	// 5 sent, 2 suppressed.
	if len(*bodies) != 5 {
		t.Fatalf("setup: got %d sent, want 5", len(*bodies))
	}
	// Advance past the hour window so prior 5 expire.
	later := now.Add(90 * time.Minute)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, "after hour", later)
	if len(*bodies) != 6 {
		t.Fatalf("post-hour send: got %d total, want 6", len(*bodies))
	}
	last := (*bodies)[5]
	if !strings.Contains(last, "after hour") {
		t.Errorf("last message missing body: %q", last)
	}
	if !strings.Contains(last, "2 additional events suppressed") {
		t.Errorf("last message missing aggregation footer: %q", last)
	}
}

func TestSendStructured_MuteHours_InfoSuppressed(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	mw, _ := parseMuteHours("00:00-08:00", time.UTC)
	n.muteWindow = mw

	// 03:00 UTC — inside mute window.
	muted := time.Date(2026, 5, 8, 3, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityInfo, "should be suppressed", muted)
	if len(*bodies) != 0 {
		t.Errorf("INFO during mute hours: got %d sent, want 0", len(*bodies))
	}

	// 12:00 UTC — outside mute window.
	awake := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityInfo, "should fire", awake)
	if len(*bodies) != 1 {
		t.Errorf("INFO outside mute hours: got %d sent, want 1", len(*bodies))
	}
}

func TestSendStructured_MuteHours_WarnTagged(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	mw, _ := parseMuteHours("00:00-08:00", time.UTC)
	n.muteWindow = mw

	muted := time.Date(2026, 5, 8, 3, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, "warn body", muted)
	if len(*bodies) != 1 {
		t.Fatalf("WARN during mute: got %d sent, want 1 (WARN is partial-mute)", len(*bodies))
	}
	if !strings.Contains((*bodies)[0], "[QUIET]") {
		t.Errorf("WARN during mute hours missing [QUIET] tag: %q", (*bodies)[0])
	}
}

func TestSendStructured_MuteHours_CriticalAlwaysSent(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	mw, _ := parseMuteHours("00:00-08:00", time.UTC)
	n.muteWindow = mw

	muted := time.Date(2026, 5, 8, 3, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "crit body", muted)
	if len(*bodies) != 1 {
		t.Fatalf("CRITICAL during mute: got %d sent, want 1 (CRITICAL never muted)", len(*bodies))
	}
	// CRITICAL must NOT have a [QUIET] tag.
	if strings.Contains((*bodies)[0], "[QUIET]") {
		t.Errorf("CRITICAL during mute hours wrongly tagged [QUIET]: %q", (*bodies)[0])
	}
}

func TestParseMuteHours_Valid(t *testing.T) {
	mw, err := parseMuteHours("22:00-08:00", time.UTC)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if mw.startHour != 22 || mw.endHour != 8 {
		t.Errorf("parsed hours wrong: got %d-%d, want 22-8", mw.startHour, mw.endHour)
	}
}

func TestParseMuteHours_Empty_Disabled(t *testing.T) {
	mw, err := parseMuteHours("", time.UTC)
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if mw != nil {
		t.Errorf("empty input should return nil window, got %+v", mw)
	}
}

func TestParseMuteHours_Invalid(t *testing.T) {
	cases := []string{
		"garbage",
		"25:00-08:00",      // hour out of range
		"22:00",            // missing range
		"22-08",            // missing minutes
		"abc:def-ghi:jkl",  // non-numeric
	}
	for _, c := range cases {
		if _, err := parseMuteHours(c, time.UTC); err == nil {
			t.Errorf("expected error for %q, got nil", c)
		}
	}
}

func TestMuteWindow_CrossMidnight(t *testing.T) {
	mw, _ := parseMuteHours("22:00-08:00", time.UTC)
	cases := []struct {
		hour int
		want bool
	}{
		{0, true},   // midnight
		{3, true},   // middle of night
		{7, true},   // just before end
		{8, false},  // exactly end (exclusive)
		{12, false}, // mid-day
		{21, false}, // just before start
		{22, true},  // exactly start (inclusive)
		{23, true},  // late evening
	}
	for _, c := range cases {
		ts := time.Date(2026, 5, 8, c.hour, 0, 0, 0, time.UTC)
		if got := mw.inMute(ts); got != c.want {
			t.Errorf("hour %d: inMute=%v, want %v", c.hour, got, c.want)
		}
	}
}

func TestMuteWindow_NilSafe(t *testing.T) {
	var mw *muteWindow
	if mw.inMute(time.Now()) {
		t.Error("nil muteWindow should never report inMute")
	}
}

func TestRateLimiter_GoroutineSafe(t *testing.T) {
	urlFmt, bodies, cleanup := startCaptureServer()
	defer cleanup()
	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	// 100 concurrent goroutines, each sending 1 WARN. Cap is 5/hour, so most
	// should be suppressed. Goal: no race / panic / data corruption.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, fmt.Sprintf("g %d", i), now.Add(time.Duration(i)*time.Millisecond))
		}(i)
	}
	wg.Wait()

	// Exactly 5 WARN sent (cap respected), 95 suppressed.
	if len(*bodies) != 5 {
		t.Errorf("concurrent WARN: got %d sent, want 5 (cap)", len(*bodies))
	}
}

