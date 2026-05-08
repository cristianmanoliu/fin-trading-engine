package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

const telegramAPIURL = "https://api.telegram.org/bot%s/sendMessage"

// Severity classifies an alert per the locked tier design (see
// results/telegram_alert_design_decision_rule_2026-05-08.md). Each tier
// has different rate limits and mute-hour behavior:
//
//   SeverityInfo     ℹ  10/hour   fully muted during mute hours
//   SeverityWarn     ⚠   5/hour   sent with [QUIET] prefix during mute hours
//   SeverityCritical 🚨  unlimited never muted
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
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
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
// prefix. Best-effort delivery — a failed POST is logged via slog.Warn and
// returned, but the caller is expected to ignore it.
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

	return sendTo(ctx, n, urlFmt, msg)
}

// sendTo is the internal implementation; urlFmt lets tests inject a test server URL.
func sendTo(ctx context.Context, n *Notifier, urlFmt string, msg string) error {
	if n.BotToken == "" || n.ChatID == "" {
		return nil
	}

	body, err := json.Marshal(map[string]string{
		"chat_id":    n.ChatID,
		"text":       msg,
		"parse_mode": "Markdown",
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
		slog.Warn("telegram send failed", "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		err = fmt.Errorf("telegram API %d", resp.StatusCode)
		slog.Warn("telegram send failed", "status", resp.StatusCode)
		return err
	}

	return nil
}

// ── rate limiter ─────────────────────────────────────────────────────────────

// rateLimiter tracks per-tier sliding-window send counts. All operations are
// goroutine-safe — the engine has multiple Stub instances + watchdogs that
// may emit alerts concurrently.
type rateLimiter struct {
	mu          sync.Mutex
	history     map[Severity][]time.Time // timestamps of sends in the last hour
	suppressed  map[Severity]int         // suppressed events since the last aggregation
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
