package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const telegramAPIURL = "https://api.telegram.org/bot%s/sendMessage"

// Notifier sends messages to a Telegram chat. When BotToken or ChatID is empty,
// Send is a no-op — same opt-in pattern as JournalPath in pkg/execution/stub.go.
type Notifier struct {
	BotToken   string
	ChatID     string
	HTTPClient *http.Client
}

// FromEnv constructs a Notifier from TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID.
// If either is unset, returns a disabled notifier (Send is a no-op).
func FromEnv() *Notifier {
	return &Notifier{
		BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Send posts msg to the configured Telegram chat. Returns an error on failure;
// the caller may ignore it — a Telegram outage must not stall the engine.
func (n *Notifier) Send(ctx context.Context, msg string) error {
	return sendTo(ctx, n, telegramAPIURL, msg)
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
