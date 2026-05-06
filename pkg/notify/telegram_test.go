package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
