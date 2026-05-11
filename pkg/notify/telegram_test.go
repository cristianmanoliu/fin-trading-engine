package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

// ── Retry + redaction tests ──────────────────────────────────────────────────
//
// These cover two production failures observed 2026-05-08:
//   1. Synchronized fleet restart produced 8 concurrent CRITICAL recovery
//      alerts; 5/16 timed out against api.telegram.org and were dropped
//      with no retry. Tier-aware retry now compensates.
//   2. Go's net/http embeds the bot token in transport error strings; the
//      previous code logged that err verbatim, leaking the token into the
//      VPS log file. redactErr now strips it at every slog/return site.

// captureSlog redirects slog.Default into a buffer for assertion. Restored
// on test cleanup. Level is Debug so any tier's output is captured.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// withFastRetryBackoff scales retryBackoffBase to 5ms for the duration of
// the test. Without this, CRITICAL retry tests would each take 3+ seconds.
func withFastRetryBackoff(t *testing.T) {
	t.Helper()
	old := retryBackoffBase
	retryBackoffBase = 5 * time.Millisecond
	t.Cleanup(func() { retryBackoffBase = old })
}

// flakyServer returns 500 for the first failBefore requests, then 200.
// Useful for testing retry-until-success paths.
func flakyServer(failBefore int32) (urlFmt string, attempts *int32, cleanup func()) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		if n <= failBefore {
			http.Error(w, "internal", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	return srv.URL + "/bot%s/sendMessage", &counter, srv.Close
}

// failingServer always returns the configured status code.
func failingServer(status int) (urlFmt string, attempts *int32, cleanup func()) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "fail", status)
	}))
	return srv.URL + "/bot%s/sendMessage", &counter, srv.Close
}

func TestSendStructured_Critical_RetriesOnTransient5xx(t *testing.T) {
	withFastRetryBackoff(t)
	urlFmt, attempts, cleanup := flakyServer(2) // 500, 500, 200
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	err := n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "recovery alert", now)
	if err != nil {
		t.Fatalf("expected eventual success after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(attempts); got != 3 {
		t.Errorf("CRITICAL on flaky 5xx: attempts = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestSendStructured_Warn_RetriesOnce(t *testing.T) {
	withFastRetryBackoff(t)
	urlFmt, attempts, cleanup := flakyServer(1) // 500, 200
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	err := n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, "stale feed", now)
	if err != nil {
		t.Fatalf("expected success after 1 retry, got: %v", err)
	}
	if got := atomic.LoadInt32(attempts); got != 2 {
		t.Errorf("WARN on flaky 5xx: attempts = %d, want 2 (initial + 1 retry)", got)
	}
}

func TestSendStructured_Info_NoRetry(t *testing.T) {
	withFastRetryBackoff(t)
	urlFmt, attempts, cleanup := failingServer(500)
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityInfo, "noop ping", now)
	// 1 attempt regardless of failure — INFO is fire-and-forget.
	if got := atomic.LoadInt32(attempts); got != 1 {
		t.Errorf("INFO on persistent failure: attempts = %d, want 1 (no retry)", got)
	}
}

func TestSendStructured_Critical_4xxNotRetried(t *testing.T) {
	withFastRetryBackoff(t)
	urlFmt, attempts, cleanup := failingServer(401) // unauthorized → don't retry
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	err := n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "test", now)
	if err == nil {
		t.Error("expected 401 to surface as error")
	}
	// Even CRITICAL must not retry on permanent 4xx — retries won't fix
	// auth or invalid chat_id, just hammer Telegram pointlessly.
	if got := atomic.LoadInt32(attempts); got != 1 {
		t.Errorf("CRITICAL on 4xx: attempts = %d, want 1 (4xx is permanent, no retry)", got)
	}
}

func TestSendStructured_Critical_FinalFailureLogsErrorLevel(t *testing.T) {
	withFastRetryBackoff(t)
	buf := captureSlog(t)
	urlFmt, _, cleanup := failingServer(500)
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "test", now)

	// Per locked tier rule + post_deploy_check.sh section 5: lost CRITICAL
	// alerts MUST be logged at ERROR level so the operator notices via the
	// post-deploy ERROR-events check.
	out := buf.String()
	if !strings.Contains(out, `"level":"ERROR"`) {
		t.Errorf("expected ERROR-level slog for lost CRITICAL, got:\n%s", out)
	}
	if !strings.Contains(out, "CRITICAL alert lost") {
		t.Errorf("expected 'CRITICAL alert lost' phrase in slog message, got:\n%s", out)
	}
}

func TestSendStructured_Warn_FinalFailureLogsWarnLevel(t *testing.T) {
	withFastRetryBackoff(t)
	buf := captureSlog(t)
	urlFmt, _, cleanup := failingServer(500)
	defer cleanup()

	n := newTestNotifier()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityWarn, "test", now)

	out := buf.String()
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Errorf("expected WARN-level slog for lost WARN, got:\n%s", out)
	}
	// Must NOT use the CRITICAL-specific phrasing.
	if strings.Contains(out, "CRITICAL alert lost") {
		t.Errorf("WARN failure wrongly tagged as CRITICAL alert lost: %s", out)
	}
}

func TestSendStructured_BotTokenNeverInSlog(t *testing.T) {
	withFastRetryBackoff(t)
	buf := captureSlog(t)
	// Server that closes the connection abruptly — produces a transport
	// error from Go's http.Client whose .Error() string includes the URL
	// (and therefore the bot token). This is the production leak vector.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Close() // abrupt close → http.Client returns wrapped err with URL
	}))
	defer srv.Close()

	const token = "TELEGRAM_BOT_TOKEN_REDACTED"
	n := &Notifier{
		BotToken:    token,
		ChatID:      "chat",
		HTTPClient:  &http.Client{Timeout: 1 * time.Second},
		rateLimiter: newRateLimiter(),
	}
	urlFmt := srv.URL + "/bot%s/sendMessage"
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	_ = n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "test", now)

	out := buf.String()
	if strings.Contains(out, token) {
		t.Errorf("bot token leaked into slog output:\n%s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("expected REDACTED placeholder in slog, got:\n%s", out)
	}
}

func TestSendStructured_BotTokenNeverInReturnedError(t *testing.T) {
	withFastRetryBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	const token = "TELEGRAM_BOT_TOKEN_REDACTED"
	n := &Notifier{
		BotToken:    token,
		ChatID:      "chat",
		HTTPClient:  &http.Client{Timeout: 1 * time.Second},
		rateLimiter: newRateLimiter(),
	}
	urlFmt := srv.URL + "/bot%s/sendMessage"
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	err := n.sendStructuredAt(context.Background(), urlFmt, SeverityCritical, "test", now)
	if err == nil {
		t.Fatal("expected error from closed connection")
	}
	if strings.Contains(err.Error(), token) {
		// Defense-in-depth: even if a caller logs the returned err themselves,
		// the token must not be in it.
		t.Errorf("bot token leaked into returned error: %v", err)
	}
}

func TestRedactErr_NilSafe(t *testing.T) {
	n := &Notifier{BotToken: "any"}
	if got := n.redactErr(nil); got != nil {
		t.Errorf("redactErr(nil) = %v, want nil", got)
	}
}

func TestRedactErr_EmptyTokenIsPassThrough(t *testing.T) {
	// When the Notifier has no token configured (e.g. legacy zero-value),
	// redactErr is a no-op — preserves error wrapping for errors.Is/As.
	n := &Notifier{} // BotToken empty
	original := errors.New("network down")
	got := n.redactErr(original)
	if got != original {
		t.Errorf("redactErr with empty token should return original err untouched, got %v", got)
	}
}

func TestRedactErr_ReplacesAllOccurrences(t *testing.T) {
	const token = "abc123:secret"
	n := &Notifier{BotToken: token}
	in := errors.New("Post \"https://api.telegram.org/bot" + token + "/sendMessage\" failed; bot" + token + " unreachable")
	out := n.redactErr(in)
	if strings.Contains(out.Error(), token) {
		t.Errorf("token still present after redact: %v", out)
	}
	if !strings.Contains(out.Error(), "REDACTED") {
		t.Errorf("expected REDACTED placeholder, got: %v", out)
	}
}

func TestIsPermanent_Classification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
		desc string
	}{
		{nil, false, "nil err"},
		{&telegramAPIError{StatusCode: 400}, true, "400 bad request"},
		{&telegramAPIError{StatusCode: 401}, true, "401 unauthorized (bad token)"},
		{&telegramAPIError{StatusCode: 404}, true, "404 not found"},
		{&telegramAPIError{StatusCode: 429}, true, "429 too many requests (still 4xx; Telegram rejects)"},
		{&telegramAPIError{StatusCode: 500}, false, "500 internal server"},
		{&telegramAPIError{StatusCode: 502}, false, "502 bad gateway"},
		{&telegramAPIError{StatusCode: 503}, false, "503 service unavailable"},
		{errors.New("net/http: request canceled"), false, "transport error (retry-eligible)"},
		{context.DeadlineExceeded, false, "context deadline (retry-eligible)"},
	}
	for _, c := range cases {
		if got := isPermanent(c.err); got != c.want {
			t.Errorf("%s: isPermanent = %v, want %v", c.desc, got, c.want)
		}
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


// TestTruncateTelegramText pins the T10 fix — Telegram's 4096-char text
// limit. Pre-fix, an oversized body returned HTTP 400 on every retry
// and the alert silently dropped. Same silent-on-corrupt-input pattern
// as the bash lib/notify.sh fix, locked here at the Go-side boundary.
func TestTruncateTelegramText(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantLen  int
		wantSame bool
		wantTail string
	}{
		{
			name:     "short text unchanged",
			input:    "hello",
			wantLen:  5,
			wantSame: true,
		},
		{
			name:     "exactly 4096 runes unchanged",
			input:    strings.Repeat("a", 4096),
			wantLen:  4096,
			wantSame: true,
		},
		{
			name:     "4097 truncated to 4096",
			input:    strings.Repeat("b", 4097),
			wantLen:  4096,
			wantSame: false,
			wantTail: "[truncated]",
		},
		{
			name:     "10000 truncated to 4096",
			input:    strings.Repeat("c", 10000),
			wantLen:  4096,
			wantSame: false,
			wantTail: "[truncated]",
		},
		{
			name:     "preserves prefix when truncated",
			input:    "BEGIN-OF-MSG" + strings.Repeat("d", 5000),
			wantLen:  4096,
			wantSame: false,
			wantTail: "[truncated]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTelegramText(tc.input)
			gotLen := len([]rune(got))
			if gotLen != tc.wantLen {
				t.Errorf("rune len = %d, want %d", gotLen, tc.wantLen)
			}
			if tc.wantSame && got != tc.input {
				t.Errorf("expected unchanged, got modified")
			}
			if !tc.wantSame && !strings.Contains(got, tc.wantTail) {
				t.Errorf("expected truncation marker %q not present in %q",
					tc.wantTail, got[max(len(got)-30, 0):])
			}
		})
	}
}

// TestTruncateTelegramText_PreservesPrefix verifies the START of the
// message is kept, not just the end (so the operator sees the most
// important context — typically the subject line + first lines of body).
func TestTruncateTelegramText_PreservesPrefix(t *testing.T) {
	prefix := "🚨 [CRITICAL] subject line\nfirst important line\n"
	bulk := strings.Repeat("filler ", 1000)
	input := prefix + bulk
	got := truncateTelegramText(input)
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("truncation did not preserve message prefix; got start = %q", got[:min(len(prefix), len(got))])
	}
}
