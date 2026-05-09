package main

import (
	"strings"
	"testing"
)

// Pure-function unit tests for validateExecutorArgs. The CLI tests in
// cli_test.go exercise the same surface end-to-end via subprocess, but
// these run in microseconds and isolate per-case behavior cleanly.

func TestValidateExecutorArgs_StubAlwaysOK(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "")
	t.Setenv("BINANCE_API_SECRET", "")
	for _, mode := range []string{"stub", ""} {
		if err := validateExecutorArgs(mode, ""); err != nil {
			t.Errorf("stub mode %q should not require creds, got error: %v", mode, err)
		}
	}
}

func TestValidateExecutorArgs_BinanceLiveRequiresBothCreds(t *testing.T) {
	cases := []struct {
		name           string
		key, secret    string
		wantErr        bool
		wantInMessage  string
	}{
		{"both unset", "", "", true, "BINANCE_API_KEY"},
		{"only key", "k", "", true, "BINANCE_API_KEY"},
		{"only secret", "", "s", true, "BINANCE_API_KEY"},
		{"both set", "k", "s", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("BINANCE_API_KEY", c.key)
			t.Setenv("BINANCE_API_SECRET", c.secret)
			err := validateExecutorArgs("binance_live", "")
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected nil error, got: %v", err)
			}
			if c.wantInMessage != "" && err != nil &&
				!strings.Contains(err.Error(), c.wantInMessage) {
				t.Errorf("error %q should contain %q", err.Error(), c.wantInMessage)
			}
		})
	}
}

func TestValidateExecutorArgs_BinanceLiveTestnetRequiresBothCreds(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "")
	t.Setenv("BINANCE_API_SECRET", "")
	err := validateExecutorArgs("binance_live_testnet", "")
	if err == nil {
		t.Fatal("expected error on testnet mode without creds")
	}
	if !strings.Contains(err.Error(), "binance_live_testnet") {
		t.Errorf("error should name the executor mode, got: %v", err)
	}
}

func TestValidateExecutorArgs_InvalidExecutorMode(t *testing.T) {
	for _, mode := range []string{"bogus", "BINANCE_LIVE", "stubz", "live"} {
		err := validateExecutorArgs(mode, "")
		if err == nil {
			t.Errorf("mode %q should be invalid", mode)
			continue
		}
		if !strings.Contains(err.Error(), "invalid --executor") {
			t.Errorf("mode %q error should say 'invalid --executor', got: %v", mode, err)
		}
	}
}

func TestValidateExecutorArgs_Layer3RequiresStubPrimary(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "k")
	t.Setenv("BINANCE_API_SECRET", "s")

	// Stub primary + Layer 3 → OK.
	if err := validateExecutorArgs("stub", "/tmp/foo"); err != nil {
		t.Errorf("stub primary + Layer 3 should be valid, got: %v", err)
	}
	// Empty primary + Layer 3 → OK (empty defaults to stub).
	if err := validateExecutorArgs("", "/tmp/foo"); err != nil {
		t.Errorf("empty primary (default stub) + Layer 3 should be valid, got: %v", err)
	}
	// Real-money primary + Layer 3 → blocked.
	err := validateExecutorArgs("binance_live", "/tmp/foo")
	if err == nil {
		t.Fatal("binance_live primary + Layer 3 should be blocked")
	}
	if !strings.Contains(err.Error(), "requires --executor=stub") {
		t.Errorf("Layer 3 + binance_live error should mention stub requirement, got: %v", err)
	}
	// Testnet primary + Layer 3 → also blocked (only stub allowed).
	err = validateExecutorArgs("binance_live_testnet", "/tmp/foo")
	if err == nil {
		t.Fatal("binance_live_testnet primary + Layer 3 should be blocked")
	}
}

func TestValidateExecutorArgs_Layer3RequiresCreds(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "")
	t.Setenv("BINANCE_API_SECRET", "")
	err := validateExecutorArgs("stub", "/tmp/foo")
	if err == nil {
		t.Fatal("Layer 3 without creds should error")
	}
	if !strings.Contains(err.Error(), "BINANCE_API_KEY") {
		t.Errorf("Layer 3 missing-creds error should name the env var, got: %v", err)
	}
}

func TestValidateExecutorArgs_NoLayer3_NoCredCheck(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "")
	t.Setenv("BINANCE_API_SECRET", "")
	// stub + no Layer 3 → no creds needed.
	if err := validateExecutorArgs("stub", ""); err != nil {
		t.Errorf("stub mode without Layer 3 should not require creds, got: %v", err)
	}
}
