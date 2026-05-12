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

// TestTargetRRDivergence pins the full input matrix for the D5 (2026-05-12)
// spec-divergence detector. The function is pure (no side effects, no
// logger/notifier dependency) so the matrix is dense.
//
// Locked invariants verified:
//
//   - YAML 6.0 + no CLI → no warning (production path on configs/default.yaml)
//   - YAML 6.0 + CLI 6.0 → no warning (production path on per-symbol YAMLs)
//   - YAML 5.0 + no CLI → WARN (Option-C YAML leak; forward-paper invalidation risk)
//   - YAML 5.0 + CLI 6.0 → no warning (deploy path: --target-rr 6.0 corrects)
//   - YAML 2.0 + no CLI → WARN (pre-D5 default.yaml; backstops the D5 file fix)
//   - YAML 0  + no CLI → WARN about fallback (T14's original silent-fallback path)
//   - YAML 0  + CLI 6.0 → no warning (CLI propagates locked value)
//   - YAML 6.0 + CLI 4.0 → WARN (operator deliberately running non-locked)
//   - YAML negative → WARN about fallback (defensive — same shape as 0)
//
// Format invariants: WARN text must NAME the locked value (6.0) AND the
// YAML path. Operators reading Telegram alerts need both to act.
func TestTargetRRDivergence(t *testing.T) {
	cases := []struct {
		name     string
		yamlVal  float64
		cliVal   float64
		wantWarn bool
		contains []string // substrings that must appear in the warning when wantWarn is true
	}{
		{"yaml6_cli0_production_default", 6.0, 0, false, nil},
		{"yaml6_cli6_production_per_symbol", 6.0, 6.0, false, nil},
		{"yaml5_cli0_option_c_leak", 5.0, 0, true,
			[]string{"5.00", "6.0", "diverges", "test.yaml"}},
		{"yaml5_cli6_deploy_corrected", 5.0, 6.0, false, nil},
		{"yaml2_cli0_pre_d5_default", 2.0, 0, true,
			[]string{"2.00", "6.0", "diverges"}},
		{"yaml0_cli0_silent_fallback", 0, 0, true,
			[]string{"fall back", "6.0"}},
		{"yaml0_cli6_cli_propagates", 0, 6.0, false, nil},
		{"yaml6_cli4_operator_research", 6.0, 4.0, true,
			[]string{"4.00", "6.0", "diverges"}},
		{"yaml_negative", -1.0, 0, true,
			[]string{"fall back", "6.0"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := targetRRDivergence(c.yamlVal, c.cliVal, "test.yaml")
			if c.wantWarn && msg == "" {
				t.Fatalf("expected warning but got empty (yaml=%.2f cli=%.2f)",
					c.yamlVal, c.cliVal)
			}
			if !c.wantWarn && msg != "" {
				t.Fatalf("expected no warning but got: %q (yaml=%.2f cli=%.2f)",
					msg, c.yamlVal, c.cliVal)
			}
			for _, sub := range c.contains {
				if !strings.Contains(msg, sub) {
					t.Errorf("warning missing required substring %q\n  got: %q", sub, msg)
				}
			}
		})
	}
}

// TestTargetRRDivergence_LockedValue is the belt-and-suspenders check
// matching scripts/test_criterion_coverage.py's LockedValuesSanityTest.
// If lockedTargetRR changes here without CLAUDE.md + LOCKED dict + per-symbol
// YAML notes updating in lockstep, the silent-drift recurrence risk returns.
func TestTargetRRDivergence_LockedValue(t *testing.T) {
	if lockedTargetRR != 6.0 {
		t.Fatalf("lockedTargetRR=%.2f but CLAUDE.md locks the candidate strategy at 6:1 RR. "+
			"A change here requires synchronized updates to:\n"+
			"  - CLAUDE.md '## Strategy status' Live config + locked RR text\n"+
			"  - scripts/test_criterion_coverage.py LOCKED['TARGET_RR_FALLBACK']\n"+
			"  - pkg/strategy/entry.go fallback constants (7 sites)\n"+
			"  - configs/default.yaml target_rr (D5 — should match locked)\n"+
			"  - This test",
			lockedTargetRR)
	}
}
