package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const telegramAPIURL = "https://api.telegram.org/bot%s/sendMessage"

// maxTelegramTextLen is the Telegram API hard limit on the `text`
// parameter — 4096 characters after entity parsing (per
// https://core.telegram.org/bots/api#sendmessage). A body exceeding
// this is rejected with HTTP 400; the retry loop hits the same too-long
// body each attempt; the alert silently drops. Same silent-on-corrupt-
// input pattern as the bash side's _notify_truncate_text — same fix
// applied here to lock the contract for every SendStructured caller.
const maxTelegramTextLen = 4096

// truncateTelegramText returns text truncated to maxTelegramTextLen
// runes (≈ Telegram chars). Telegram counts UTF-16 code units after
// entity parsing; for simplicity we count runes (Unicode code points)
// which yields a slightly conservative truncation when the input
// contains supplementary-plane characters (emoji surrogate pairs).
// The conservatism is intentional — better to under-fill the message
// by a few chars than to be rejected for over-size.
func truncateTelegramText(text string) string {
	runes := []rune(text)
	if len(runes) <= maxTelegramTextLen {
		return text
	}
	const marker = "\n…[truncated]"
	markerRunes := []rune(marker)
	keep := max(maxTelegramTextLen-len(markerRunes), 0)
	return string(runes[:keep]) + marker
}

// Severity classifies an alert per the locked tier design (see
// results/telegram_alert_design_decision_rule_2026-05-08.md). Each tier
// has different rate limits and mute-hour behavior:
//
//	SeverityInfo     ℹ  10/hour   fully muted during mute hours
//	SeverityWarn     ⚠   5/hour   sent with [QUIET] prefix during mute hours
//	SeverityCritical 🚨  unlimited never muted
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityCritical
)

func (s Severity) prefix() string {
	switch s {
	case SeverityInfo:
		return "ℹ"
	case SeverityWarn:
		return "⚠"
	case SeverityCritical:
		return "🚨"
	default:
		return "?"
	}
}

// hourlyLimit returns the per-hour cap for the tier. -1 means unlimited.
func (s Severity) hourlyLimit() int {
	switch s {
	case SeverityInfo:
		return 10
	case SeverityWarn:
		return 5
	case SeverityCritical:
		return -1
	default:
		return 0
	}
}

// Notifier sends messages to a Telegram chat. When BotToken or ChatID is empty,
// Send is a no-op — same opt-in pattern as JournalPath in pkg/execution/stub.go.
type Notifier struct {
	BotToken   string
	ChatID     string
	HTTPClient *http.Client

	// rateLimiter and muteWindow govern SendStructured's per-tier behavior.
	// Both are nil-safe: a Notifier built via the zero value (e.g. in older tests)
	// works for plain Send, and SendStructured falls back to "always allow,
	// never mute" when these are nil.
	rateLimiter *rateLimiter
	muteWindow  *muteWindow
}

// FromEnv constructs a Notifier from TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID.
// If either is unset, returns a disabled notifier (Send is a no-op).
// TELEGRAM_MUTE_HOURS_LOCAL of the form "HH:MM-HH:MM" enables mute-window
// suppression for INFO/WARN; if unset or unparseable, mute is disabled.
func FromEnv() *Notifier {
	n := &Notifier{
		BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		// Per-request timeout intentionally generous — under synchronized
		// fleet restart (16 engines posting CRITICAL recovery alerts in the
		// same second), a 5s timeout fired before api.telegram.org responded
		// and dropped 5/16 alerts. 2026-05-08T15:00 incident verified.
		// Combined with tier-aware retry in sendWithRetry, 10s gives Telegram
		// time to flush a synchronized burst before declaring the attempt failed.
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		rateLimiter: newRateLimiter(),
	}
	if mw, err := parseMuteHours(os.Getenv("TELEGRAM_MUTE_HOURS_LOCAL"), time.Local); err != nil {
		slog.Warn("telegram mute window parse failed; mute disabled", "err", err)
	} else {
		n.muteWindow = mw
	}
	return n
}

// Send posts msg to the configured Telegram chat. Returns an error on failure;
// the caller may ignore it — a Telegram outage must not stall the engine.
//
// This is the unstructured legacy path. For structured per-tier alerts use
// SendStructured.
func (n *Notifier) Send(ctx context.Context, msg string) error {
	return sendTo(ctx, n, telegramAPIURL, msg)
}

// SendStructured emits an alert with locked tier semantics from the design
// rule: rate limiting per tier, mute-hour suppression for INFO/WARN, format
// prefix. Per-tier retry policy on transient failures (see retryAttempts).
// Final failure for CRITICAL logs via slog.Error per the locked rule; lower
// tiers via slog.Warn. All log + return paths redact the bot token.
func (n *Notifier) SendStructured(ctx context.Context, severity Severity, body string) error {
	return n.sendStructuredAt(ctx, telegramAPIURL, severity, body, time.Now())
}

// sendStructuredAt is the testable form — clock + URL injectable.
func (n *Notifier) sendStructuredAt(ctx context.Context, urlFmt string, severity Severity, body string, now time.Time) error {
	if n.BotToken == "" || n.ChatID == "" {
		return nil
	}

	// Mute-window check: INFO fully suppressed; WARN tagged; CRITICAL ignores mute.
	inMute := n.muteWindow != nil && n.muteWindow.inMute(now)
	if inMute && severity == SeverityInfo {
		return nil
	}

	// Rate-limit check: returns (allow, suppressed_count_at_aggregation).
	allow, suppressed := true, 0
	if n.rateLimiter != nil {
		allow, suppressed = n.rateLimiter.allow(severity, now)
	}
	if !allow {
		return nil
	}

	// Build message with prefix + optional [QUIET] tag + optional aggregation footer.
	quietTag := ""
	if inMute && severity == SeverityWarn {
		quietTag = "[QUIET] "
	}
	msg := fmt.Sprintf("%s %s%s", severity.prefix(), quietTag, body)
	if suppressed > 0 {
		msg += fmt.Sprintf("\n(%d additional events suppressed in last hour)", suppressed)
	}
	// Telegram-size truncation: see maxTelegramTextLen above. Without
	// this, an oversized body returns HTTP 400 on every retry and the
	// alert silently drops. Same family as bash lib/notify.sh fix.
	msg = truncateTelegramText(msg)

	return n.sendWithRetry(ctx, urlFmt, msg, severity)
}

// sendTo is the legacy single-attempt path used by Send (unstructured).
// It does NOT retry — the locked tier rule's retry contract is bound to
// SendStructured. sendTo still benefits from token redaction.
func sendTo(ctx context.Context, n *Notifier, urlFmt string, msg string) error {
	if n.BotToken == "" || n.ChatID == "" {
		return nil
	}
	if err := sendOnce(ctx, n, urlFmt, msg); err != nil {
		sanitized := n.redactErr(err)
		slog.Warn("telegram send failed", "err", sanitized)
		return sanitized
	}
	return nil
}

// retryBackoffBase is the starting backoff between retry attempts. Each
// subsequent retry doubles. Exposed as a var (not const) so tests can scale
// it down to milliseconds without inflating runtime by 3+ seconds per case.
var retryBackoffBase = 1 * time.Second

// retryAttempts encodes the per-tier retry policy. CRITICAL must reach the
// operator (recovery, drift, kill events) so it gets the most attempts;
// INFO is fire-and-forget. Failures past the budget are logged via slog at
// the matching level — slog.Error for CRITICAL per the locked tier rule,
// slog.Warn otherwise.
//
//	CRITICAL: 3 attempts (initial + 2 retries) with backoff 1s, 2s + jitter
//	WARN:     2 attempts (initial + 1 retry)  with backoff 1s + jitter
//	INFO:     1 attempt  (no retry)
func retryAttempts(s Severity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarn:
		return 2
	default:
		return 1
	}
}

// sendWithRetry orchestrates per-tier retry around sendOnce. Permanent
// failures (4xx) skip retry. Synchronized fleet restarts produce a burst
// of POSTs that occasionally trip the per-request timeout; the jittered
// backoff de-syncs the retry wave so a thundering herd does not retry in
// lockstep. All log + return paths redact the bot token.
func (n *Notifier) sendWithRetry(ctx context.Context, urlFmt string, msg string, severity Severity) error {
	attempts := retryAttempts(severity)
	backoff := retryBackoffBase
	var lastErr error

	for i := 0; i < attempts; i++ {
		if i > 0 {
			// Jitter prevents 16 simultaneously-failing engines from retrying
			// in lockstep — that would re-trigger the same thundering herd
			// that caused the original failure. Randomized in [base, 2*base).
			wait := backoff + time.Duration(rand.Int64N(int64(backoff)))
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return n.redactErr(ctx.Err())
			case <-timer.C:
			}
			backoff *= 2
		}
		err := sendOnce(ctx, n, urlFmt, msg)
		if err == nil {
			return nil
		}
		lastErr = err
		if isPermanent(err) {
			break
		}
	}

	sanitized := n.redactErr(lastErr)
	if severity == SeverityCritical {
		slog.Error("telegram send failed (CRITICAL alert lost)",
			"err", sanitized, "attempts", attempts)
	} else {
		slog.Warn("telegram send failed",
			"err", sanitized, "attempts", attempts)
	}
	return sanitized
}

// sendOnce performs a single POST without retry or slog. Returns the bare
// error which may embed the bot token in a Go net/http URL string —
// callers must run the result through redactErr before logging or returning.
func sendOnce(ctx context.Context, n *Notifier, urlFmt string, msg string) error {
	// Plain text — NO parse_mode. Alert bodies embed snake_case keys, CLI
	// flags and env-var names (ws_url, lag_p50_ms, BINANCE_API_KEY); with
	// parse_mode=Markdown a lone underscore is an unterminated-italic entity
	// and Telegram rejects the whole message with 400 (the 2026-06-10
	// KAVAUSDT gate-block alert was lost exactly this way). No caller uses
	// intentional Markdown. Contract locked by TestSendBody_NoParseMode.
	body, err := json.Marshal(map[string]string{
		"chat_id": n.ChatID,
		"text":    msg,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf(urlFmt, n.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &telegramAPIError{StatusCode: resp.StatusCode}
	}
	return nil
}

// telegramAPIError is the typed error returned by sendOnce for non-2xx
// responses. Lets isPermanent classify 4xx (don't retry — auth, invalid
// chat, malformed message) versus 5xx (retry-eligible — server-side hiccup)
// without string-parsing the message.
type telegramAPIError struct {
	StatusCode int
}

func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("telegram API %d", e.StatusCode)
}

// isPermanent reports whether retry is futile. 4xx responses won't recover
// without operator intervention (rotate token, fix chat_id, malformed
// message); 5xx and network errors typically resolve within seconds.
func isPermanent(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *telegramAPIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 400 && apiErr.StatusCode < 500
	}
	return false
}

// redactErr strips the bot token from an error string so it cannot leak
// into /var/log on the VPS. Go's net/http embeds the request URL verbatim
// in transport errors (e.g. `Post "https://api.telegram.org/bot<TOK>/...":
// timeout`). When this err is logged via slog or returned to a caller that
// logs, the token is exposed to anyone with read access to the log file.
//
// Any future code path that adds err logging in this package MUST go through
// redactErr — the test suite enforces "no token in any logged err" via a
// captured slog handler.
func (n *Notifier) redactErr(err error) error {
	if err == nil || n.BotToken == "" {
		return err
	}
	s := err.Error()
	redacted := strings.ReplaceAll(s, n.BotToken, "REDACTED")
	if redacted == s {
		return err
	}
	return errors.New(redacted)
}

// ── rate limiter ─────────────────────────────────────────────────────────────

// rateLimiter tracks per-tier sliding-window send counts. All operations are
// goroutine-safe — the engine has multiple Stub instances + watchdogs that
// may emit alerts concurrently.
type rateLimiter struct {
	mu         sync.Mutex
	history    map[Severity][]time.Time // timestamps of sends in the last hour
	suppressed map[Severity]int         // suppressed events since the last aggregation
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		history:    make(map[Severity][]time.Time),
		suppressed: make(map[Severity]int),
	}
}

// allow returns (sendNow, suppressedSinceLastAggregation). If sendNow is true
// and suppressedSinceLastAggregation > 0, the caller appends an aggregation
// footer to the message. CRITICAL is always allowed (rate-limit-bypass).
func (r *rateLimiter) allow(s Severity, now time.Time) (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	limit := s.hourlyLimit()
	if limit < 0 {
		// CRITICAL: unlimited, but we still record for symmetry.
		r.history[s] = append(r.history[s], now)
		return true, 0
	}

	// Trim history older than 1 hour.
	cutoff := now.Add(-time.Hour)
	trimmed := r.history[s][:0]
	for _, t := range r.history[s] {
		if t.After(cutoff) {
			trimmed = append(trimmed, t)
		}
	}
	r.history[s] = trimmed

	if len(trimmed) < limit {
		r.history[s] = append(r.history[s], now)
		// Drain accumulated suppression count with this send.
		suppressed := r.suppressed[s]
		r.suppressed[s] = 0
		return true, suppressed
	}

	// Hit cap — block this send, increment suppression counter.
	r.suppressed[s]++
	return false, 0
}

// ── mute window ──────────────────────────────────────────────────────────────

// muteWindow defines a daily local-time window during which INFO is fully
// suppressed and WARN is tagged with a [QUIET] prefix. CRITICAL is never muted.
type muteWindow struct {
	startHour int // 0..23 in the configured location
	endHour   int // 0..23; if endHour <= startHour, the window crosses midnight
	location  *time.Location
}

// parseMuteHours parses "HH:MM-HH:MM" format. The minute fields are accepted
// but currently rounded down — mute window granularity is 1 hour.
// Empty input returns (nil, nil) — disable mute.
func parseMuteHours(env string, loc *time.Location) (*muteWindow, error) {
	if env == "" {
		return nil, nil
	}
	if loc == nil {
		loc = time.Local
	}
	var sH, sM, eH, eM int
	n, err := fmt.Sscanf(env, "%d:%d-%d:%d", &sH, &sM, &eH, &eM)
	if err != nil || n != 4 {
		return nil, fmt.Errorf("parse %q: expected HH:MM-HH:MM", env)
	}
	if sH < 0 || sH > 23 || eH < 0 || eH > 23 {
		return nil, fmt.Errorf("parse %q: hours out of range 0-23", env)
	}
	return &muteWindow{startHour: sH, endHour: eH, location: loc}, nil
}

// inMute reports whether the given time falls within the mute window. Handles
// cross-midnight windows (e.g., 22:00-08:00).
func (m *muteWindow) inMute(t time.Time) bool {
	if m == nil {
		return false
	}
	h := t.In(m.location).Hour()
	if m.startHour <= m.endHour {
		return h >= m.startHour && h < m.endHour
	}
	// Cross-midnight: in mute if h >= start OR h < end.
	return h >= m.startHour || h < m.endHour
}
