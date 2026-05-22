package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/execution"
)

// End-to-end CLI tests for kill_switch.
//
// kill_switch is the operator-facing emergency real-money market-close-all
// (Path C of auto_kill_execution_decision_rule_2026-05-08.md). It is the
// LAST line of defense against runaway real-money positions and shipped
// with zero CLI coverage. Same shape of risk as the Layer 3 fail-open we
// closed earlier today: silent regressions in argument validation could
// only surface when the operator actually needs the tool.
//
// Tested paths:
//   - flag validation (--reason, --positions required)
//   - --positions parse propagation
//   - dry-run printout (no CONFIRM)
//   - CONFIRM mode env validation (BINANCE_API_KEY/SECRET required)
//
// NOT tested here: the actual CONFIRM-mode order send path. That requires
// either real Binance credentials (would actually send orders) or
// extensive Router/Notifier mocking — bigger scope, separate work.

var killBinary string

func TestMain(m *testing.M) {
	tmp, err := os.CreateTemp("", "kill_switch_cli_test_*")
	if err != nil {
		panic(fmt.Sprintf("create tmp binary: %v", err))
	}
	tmp.Close()
	killBinary = tmp.Name()

	build := exec.Command("go", "build", "-o", killBinary, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		os.Remove(killBinary)
		fmt.Printf("build failed: %v\n%s\n", err, out)
		os.Exit(2)
	}

	code := m.Run()
	os.Remove(killBinary)
	os.Exit(code)
}

// runKill exec's the binary with the given args and an optional env override.
// Pass nil for env to use the test process env (no Binance creds).
func runKill(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(killBinary, args...)
	if env != nil {
		cmd.Env = env
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), buf.String()
		}
		t.Fatalf("exec failed: %v\noutput:\n%s", err, buf.String())
	}
	return 0, buf.String()
}

// TestCLI_MissingReason_Exits1 — --reason is required for the kill artifact slug.
func TestCLI_MissingReason_Exits1(t *testing.T) {
	code, out := runKill(t, nil, "--positions", "BTCUSDT,LONG,0.5")
	if code != 1 {
		t.Errorf("expected exit 1 on missing --reason, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "reason is required") {
		t.Errorf("expected --reason error message, got:\n%s", out)
	}
}

// TestCLI_MissingPositions_Exits1 — --positions is required.
func TestCLI_MissingPositions_Exits1(t *testing.T) {
	code, out := runKill(t, nil, "--reason", "test_kill_2026-05-09")
	if code != 1 {
		t.Errorf("expected exit 1 on missing --positions, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "positions is required") {
		t.Errorf("expected --positions error message, got:\n%s", out)
	}
}

// TestCLI_BadPositionsSpec_Exits1 — malformed --positions surface up the
// ParseClosePositionSpec error and exit 1 before Binance contact.
func TestCLI_BadPositionsSpec_Exits1(t *testing.T) {
	code, out := runKill(t, nil,
		"--reason", "test_kill_2026-05-09",
		"--positions", "BTCUSDT,LONG") // missing qty field
	if code != 1 {
		t.Errorf("expected exit 1 on bad positions spec, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "positions parse failed") {
		t.Errorf("expected parse error message, got:\n%s", out)
	}
}

// TestCLI_DryRun_NoOrdersSent_Exits0 — without the CONFIRM positional, the
// tool prints what it would close and exits cleanly. The CRITICAL contract:
// no Binance contact happens in this path even with creds in env.
func TestCLI_DryRun_NoOrdersSent_Exits0(t *testing.T) {
	// Even with bogus creds in env, dry-run never reaches the Binance call.
	env := append(os.Environ(),
		"BINANCE_API_KEY=fakekey",
		"BINANCE_API_SECRET=fakesecret",
		"BINANCE_API_BASE=https://invalid.example.invalid", // would fail if dialed
	)
	code, out := runKill(t, env,
		"--reason", "test_kill_2026-05-09",
		"--positions", "BTCUSDT,LONG,0.5;ETHUSDT,SHORT,2.0")
	if code != 0 {
		t.Errorf("expected exit 0 on dry-run, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("expected 'DRY RUN' marker, got:\n%s", out)
	}
	// Both positions enumerated.
	if !strings.Contains(out, "BTCUSDT") || !strings.Contains(out, "ETHUSDT") {
		t.Errorf("expected both symbols in dry-run output, got:\n%s", out)
	}
	// Reason echoed.
	if !strings.Contains(out, "test_kill_2026-05-09") {
		t.Errorf("expected reason in dry-run output, got:\n%s", out)
	}
}

// TestCLI_DryRun_DefaultsToZeroQuantity — the dry-run output should show
// the parsed quantity exactly as provided (regression-guard against a
// formatting bug that drops decimals).
func TestCLI_DryRun_PreservesQuantity(t *testing.T) {
	code, out := runKill(t, nil,
		"--reason", "preserve_qty",
		"--positions", "BTCUSDT,LONG,0.5")
	if code != 0 {
		t.Fatalf("dry-run failed: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "qty=0.5") {
		t.Errorf("expected qty=0.5 in output, got:\n%s", out)
	}
}

// TestCLI_Confirm_MissingAPIKey_Exits1 — CONFIRM mode requires
// BINANCE_API_KEY. Without it, exit 1 BEFORE any pre-fire alert or order.
func TestCLI_Confirm_MissingAPIKey_Exits1(t *testing.T) {
	// Ensure no Binance creds from the operator's actual env leak in.
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"BINANCE_API_SECRET=fakesecret", // only secret set, key missing
	}
	code, out := runKill(t, env,
		"--reason", "no_api_key",
		"--positions", "BTCUSDT,LONG,0.5",
		"CONFIRM")
	if code != 1 {
		t.Errorf("expected exit 1 on CONFIRM with missing API key, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "BINANCE_API_KEY") {
		t.Errorf("expected error to name the missing env var, got:\n%s", out)
	}
}

// TestCLI_Confirm_MissingAPISecret_Exits1 — symmetric: API_SECRET required.
func TestCLI_Confirm_MissingAPISecret_Exits1(t *testing.T) {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"BINANCE_API_KEY=fakekey", // only key set, secret missing
	}
	code, out := runKill(t, env,
		"--reason", "no_api_secret",
		"--positions", "BTCUSDT,LONG,0.5",
		"CONFIRM")
	if code != 1 {
		t.Errorf("expected exit 1 on CONFIRM with missing API secret, got %d\nout:\n%s", code, out)
	}
}

// TestCLI_DryRun_BeforeConfirm_NoBinanceContact verifies that the CONFIRM
// gate is positional, not a flag. Specifically: providing the args without
// the trailing CONFIRM token must take the dry-run path even if env is
// fully populated. This is the documented operator safety contract — typos
// like "--confirm" or "Confirm" or no token at all should NEVER fire.
func TestCLI_ConfirmGateIsPositional(t *testing.T) {
	env := append(os.Environ(),
		"BINANCE_API_KEY=fakekey",
		"BINANCE_API_SECRET=fakesecret",
		"BINANCE_API_BASE=https://invalid.example.invalid",
	)
	// Try several "almost-confirm" tokens that should all be no-ops.
	for _, almostConfirm := range []string{"", "confirm", "Confirm", "yes", "y"} {
		args := []string{
			"--reason", "almost_confirm",
			"--positions", "BTCUSDT,LONG,0.5",
		}
		if almostConfirm != "" {
			args = append(args, almostConfirm)
		}
		code, out := runKill(t, env, args...)
		if code != 0 {
			t.Errorf("almost-confirm %q: expected exit 0 (dry-run), got %d\nout:\n%s",
				almostConfirm, code, out)
		}
		if !strings.Contains(out, "DRY RUN") {
			t.Errorf("almost-confirm %q: expected DRY RUN marker, got:\n%s",
				almostConfirm, out)
		}
	}
}

// TestEvaluateKillResult_PartialFillsExits2_NotSuccess is the audit-pattern
// regression for the highest-severity finding in the cmd/kill_switch audit:
// KillSwitch.KillAll returns nil err when failureCount==0, but PARTIAL
// outcomes don't increment failureCount (the exchange filled some but not
// all of the requested quantity). Pre-fix, cmd/kill_switch printed "All
// positions closed cleanly" on the killErr==nil path, hiding residual
// real-money exposure from the operator at the worst possible moment
// (post-panic-kill).
//
// The fix routes PARTIAL outcomes to a distinct exit code 2 with explicit
// residual reporting. Operator MUST close residuals manually via Binance
// UI before considering the kill complete.
func TestEvaluateKillResult_PartialFillsExits2_NotSuccess(t *testing.T) {
	// KillResult with 1 CLOSED + 1 PARTIAL — KillAll would return nil err
	// because failureCount==0 (PARTIAL doesn't count as failure).
	result := execution.KillResult{
		Reason: "drift_kill_test",
		Outcomes: []execution.KillOutcome{
			{Symbol: "BTCUSDT", Status: "CLOSED", Requested: 0.5, Filled: 0.5, AvgPrice: 50000},
			{Symbol: "ETHUSDT", Status: "PARTIAL", Requested: 2.0, Filled: 1.5, AvgPrice: 2300},
		},
	}
	exitCode, summary, partialDetails := evaluateKillResult(result, nil)

	if exitCode != 2 {
		t.Errorf("PARTIAL outcome must exit 2, got %d (summary=%q)", exitCode, summary)
	}
	if !strings.Contains(summary, "PARTIAL") {
		t.Errorf("summary must mention PARTIAL, got: %q", summary)
	}
	if len(partialDetails) != 1 {
		t.Fatalf("expected 1 partial detail, got %d", len(partialDetails))
	}
	// Residual should be 2.0 - 1.5 = 0.5
	if !strings.Contains(partialDetails[0], "ETHUSDT") {
		t.Errorf("partial detail should name ETHUSDT, got: %q", partialDetails[0])
	}
	if !strings.Contains(partialDetails[0], "residual=0.5") {
		t.Errorf("partial detail should show residual=0.5, got: %q", partialDetails[0])
	}
}

func TestEvaluateKillResult_AllClosedExits0(t *testing.T) {
	result := execution.KillResult{
		Reason: "drift_kill_test",
		Outcomes: []execution.KillOutcome{
			{Symbol: "BTCUSDT", Status: "CLOSED", Requested: 0.5, Filled: 0.5},
			{Symbol: "ETHUSDT", Status: "CLOSED", Requested: 2.0, Filled: 2.0},
		},
	}
	exitCode, summary, partialDetails := evaluateKillResult(result, nil)

	if exitCode != 0 {
		t.Errorf("all CLOSED must exit 0, got %d", exitCode)
	}
	if !strings.Contains(summary, "All positions closed cleanly") {
		t.Errorf("clean kill must produce success message, got: %q", summary)
	}
	if len(partialDetails) != 0 {
		t.Errorf("clean kill should have no partial details, got %d", len(partialDetails))
	}
}

func TestEvaluateKillResult_FailureExits1(t *testing.T) {
	result := execution.KillResult{
		Reason: "drift_kill_test",
		Outcomes: []execution.KillOutcome{
			{Symbol: "BTCUSDT", Status: "CLOSED", Requested: 0.5, Filled: 0.5},
			{Symbol: "ETHUSDT", Status: "FAILED", Requested: 2.0, Error: "insufficient margin"},
		},
	}
	killErr := fmt.Errorf("KillAll: 1 of 2 positions failed")
	exitCode, summary, _ := evaluateKillResult(result, killErr)

	if exitCode != 1 {
		t.Errorf("FAILED outcome with non-nil killErr must exit 1, got %d", exitCode)
	}
	if !strings.Contains(summary, "1 failures") {
		t.Errorf("failure message must include count, got: %q", summary)
	}
}

// TestEvaluateKillResult_PartialAndFailedExits1_FailureWins verifies the
// classification precedence: when both PARTIAL and FAILED outcomes exist,
// the failure-exit-1 wins because killErr is non-nil from KillAll.
// Operator gets the FAILURE message first; partial info is in the
// per-symbol stdout printout above the summary.
func TestEvaluateKillResult_PartialAndFailedExits1_FailureWins(t *testing.T) {
	result := execution.KillResult{
		Reason: "drift_kill_test",
		Outcomes: []execution.KillOutcome{
			{Symbol: "BTCUSDT", Status: "PARTIAL", Requested: 0.5, Filled: 0.3},
			{Symbol: "ETHUSDT", Status: "FAILED", Requested: 2.0, Error: "rate-limited"},
		},
	}
	killErr := fmt.Errorf("KillAll: 1 of 2 positions failed")
	exitCode, _, _ := evaluateKillResult(result, killErr)

	if exitCode != 1 {
		t.Errorf("any FAILED (with non-nil killErr) must exit 1 even with PARTIAL present, got %d", exitCode)
	}
}
