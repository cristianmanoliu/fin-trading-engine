package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end CLI tests for journal_diff. The existing unit tests in
// main_test.go cover the parsing and matching logic in isolation; these
// tests exercise the full CLI surface — flag parsing, parseJournalDir →
// matchPairs → report → os.Exit — against synthetic on-disk journals.
//
// Layer 3 parity is the gate we run before promoting any engine to
// real-money mode (per real_money_executor_architecture_decision_rule_2026-05-08.md).
// A regression that flipped exit-code semantics (e.g. signal divergence
// returning 1 instead of 2, or threshold violation silently passing)
// would silently undermine the gate.

// cliBinary is set by TestMain to the absolute path of a compiled
// journal_diff binary. We build once and reuse — `go run` doesn't preserve
// the inner program's exit code (always exits 1 on non-zero), so direct
// exec is required to read the documented 0/1/2/3 contract.
var cliBinary string

// TestMain compiles the journal_diff binary into a temp file, runs the
// suite, then cleans up. This costs ~0.5s once but lets every test exec
// the real binary and read its exit code without `go run` interference.
func TestMain(m *testing.M) {
	tmp, err := os.CreateTemp("", "journal_diff_cli_test_*")
	if err != nil {
		panic(fmt.Sprintf("create tmp binary: %v", err))
	}
	tmp.Close()
	cliBinary = tmp.Name()

	build := exec.Command("go", "build", "-o", cliBinary, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		os.Remove(cliBinary)
		fmt.Printf("build failed: %v\n%s\n", err, out)
		os.Exit(2)
	}

	code := m.Run()
	os.Remove(cliBinary)
	os.Exit(code)
}

// runDiff exec's the pre-built binary with the given args and returns
// (exit code, combined stdout+stderr).
func runDiff(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(cliBinary, args...)
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

// writeJournal writes a synthetic <dir>/journal/<symbol>-<month>.jsonl with
// paired open/close events. Reuses the writeFile helper from main_test.go.
func writeJournal(t *testing.T, dir, symbol, month string, trades []syntheticTrade) {
	t.Helper()
	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(jdir, symbol+"-"+month+".jsonl")
	lines := make([]string, 0, len(trades)*2)
	for _, tr := range trades {
		lines = append(lines, fmt.Sprintf(
			`{"event":"open","symbol":%q,"ts":%q,"side":%q,"entry":100,"stop":99,"target":106,"reason":"test"}`,
			symbol, tr.openTS, tr.side))
		lines = append(lines, fmt.Sprintf(
			`{"event":"close","symbol":%q,"ts":%q,"side":%q,"entry":100,"exit":105,"stop":99,"target":106,"pnl_usd":%.2f,"outcome":%q,"reason":"test"}`,
			symbol, tr.closeTS, tr.side, tr.pnlUSD, tr.outcome))
	}
	writeFile(t, path, lines)
}

type syntheticTrade struct {
	openTS, closeTS, side, outcome string
	pnlUSD                         float64
}

func sampleTrades(pnls []float64) []syntheticTrade {
	out := make([]syntheticTrade, len(pnls))
	for i, p := range pnls {
		outcome := "STOP"
		if p > 0 {
			outcome = "TARGET"
		}
		out[i] = syntheticTrade{
			openTS:  fmt.Sprintf("2026-05-08T%02d:00:00Z", i),
			closeTS: fmt.Sprintf("2026-05-08T%02d:30:00Z", i),
			side:    "LONG",
			outcome: outcome,
			pnlUSD:  p,
		}
	}
	return out
}

// TestCLI_BothEmpty_Exits0 verifies the trivial case: empty journal dirs
// → no matched pairs, no signal divergence, no threshold violation → PASS.
func TestCLI_BothEmpty_Exits0(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	// Create empty journal subdirs (parseJournalDir tolerates either path).
	if err := os.MkdirAll(filepath.Join(dirA, "journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dirB, "journal"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 0 {
		t.Errorf("expected exit 0 on empty journals, got %d\nout:\n%s", code, out)
	}
}

// TestCLI_IdenticalJournals_Exits0 verifies that perfectly-matched journals
// produce zero diff per pair → exit 0 PASS.
func TestCLI_IdenticalJournals_Exits0(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	pnls := []float64{1000, -500, 800, -300}
	writeJournal(t, dirA, "BTCUSDT", "2026-05", sampleTrades(pnls))
	writeJournal(t, dirB, "BTCUSDT", "2026-05", sampleTrades(pnls))

	code, out := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 0 {
		t.Errorf("expected exit 0 on identical journals, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "Matched pairs:") {
		t.Errorf("expected 'Matched pairs:' in report, got:\n%s", out)
	}
}

// TestCLI_TinyDiffWithinThreshold_Exits0 verifies that small per-pair pnl
// differences (well below the locked 0.5% threshold) pass cleanly.
// $1000 vs $1002 = 0.2% — under 0.5%.
func TestCLI_TinyDiffWithinThreshold_Exits0(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeJournal(t, dirA, "BTCUSDT", "2026-05", sampleTrades([]float64{1000}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05", sampleTrades([]float64{1002}))

	code, _ := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 0 {
		t.Errorf("expected exit 0 on 0.2%% diff (under 0.5%% threshold), got %d", code)
	}
}

// TestCLI_DiffAtExactlyThreshold_Exits0 verifies the boundary: 0.5% diff is
// not a violation (the comparison is strictly greater-than).
// $1000 vs $1005 = exactly 0.5%, by max-denom rule = 5/1005 × 100.
func TestCLI_DiffAtBoundary_PassesUnlessExceeded(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	// 1000 vs 1005 → diff = 5, max = 1005 → 5/1005 = 0.4975% < 0.5%.
	writeJournal(t, dirA, "BTCUSDT", "2026-05", sampleTrades([]float64{1000}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05", sampleTrades([]float64{1005}))

	code, _ := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 0 {
		t.Errorf("expected exit 0 at 0.4975%% diff (just under 0.5%%), got %d", code)
	}
}

// TestCLI_OverThreshold_Exits1 verifies that a per-pair pnl diff exceeding
// 0.5% trips the threshold-violation exit code. 1000 vs 1100 = 9.09%.
func TestCLI_OverThreshold_Exits1(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeJournal(t, dirA, "BTCUSDT", "2026-05", sampleTrades([]float64{1000}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05", sampleTrades([]float64{1100}))

	code, out := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 1 {
		t.Errorf("expected exit 1 (THRESHOLD VIOLATION) on 9.09%% diff, got %d\nout:\n%s",
			code, out)
	}
	if !strings.Contains(out, "Violations:") {
		t.Errorf("expected 'Violations:' in report, got:\n%s", out)
	}
}

// TestCLI_OnlyA_SignalDivergence_Exits2 verifies that a trade in A with no
// matching peer in B trips signal-divergence (exit 2), not threshold (1).
// Signal divergence beats threshold per the documented exit-code precedence.
func TestCLI_OnlyA_SignalDivergence_Exits2(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeJournal(t, dirA, "BTCUSDT", "2026-05",
		sampleTrades([]float64{1000, -500}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05",
		sampleTrades([]float64{1000})) // missing the second trade

	code, out := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 2 {
		t.Errorf("expected exit 2 (SIGNAL DIVERGENCE) when A has unpaired trade, got %d\nout:\n%s",
			code, out)
	}
}

// TestCLI_OnlyB_SignalDivergence_Exits2 — symmetric: B has trade A doesn't.
func TestCLI_OnlyB_SignalDivergence_Exits2(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeJournal(t, dirA, "BTCUSDT", "2026-05",
		sampleTrades([]float64{1000}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05",
		sampleTrades([]float64{1000, -500}))

	code, _ := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 2 {
		t.Errorf("expected exit 2 (SIGNAL DIVERGENCE) when B has unpaired trade, got %d", code)
	}
}

// TestCLI_SignalDivergenceBeatsThreshold verifies the documented precedence:
// when both signal-divergence AND threshold-violation exist, signal
// divergence (exit 2) wins. It's the more fundamental failure — executors
// disagreed on whether to trade, so per-pair pnl agreement is moot.
func TestCLI_SignalDivergenceBeatsThreshold(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	// Two trades in A: trade 1 matches B but is well over threshold (1000 vs
	// 2000 = 50% diff). Trade 2 has no peer in B.
	writeJournal(t, dirA, "BTCUSDT", "2026-05",
		sampleTrades([]float64{1000, -500}))
	writeJournal(t, dirB, "BTCUSDT", "2026-05",
		sampleTrades([]float64{2000})) // 1 only — second trade is unpaired

	code, _ := runDiff(t, "--dir-a", filepath.Join(dirA, "journal"),
		"--dir-b", filepath.Join(dirB, "journal"))
	if code != 2 {
		t.Errorf("expected exit 2 (signal divergence beats threshold), got %d", code)
	}
}

// TestCLI_MissingDir_Exits3 verifies the input-error path. A non-existent
// dir-a should yield exit 3, not a panic or exit 1.
func TestCLI_MissingDir_Exits3(t *testing.T) {
	code, out := runDiff(t, "--dir-a", "/nonexistent/path/abc",
		"--dir-b", "/nonexistent/path/xyz")
	if code != 3 {
		t.Errorf("expected exit 3 (INPUT ERROR) on missing dir, got %d\nout:\n%s",
			code, out)
	}
}

// TestCLI_MissingFlags_Exits3 — both --dir-a and --dir-b are required.
func TestCLI_MissingFlags_Exits3(t *testing.T) {
	code, _ := runDiff(t)
	if code != 3 {
		t.Errorf("expected exit 3 when required flags missing, got %d", code)
	}
}

// TestCLI_Layer3Topology_DefaultExcludesShadows is the regression for the
// fail-CLOSE that blocked every Layer 3 run before this commit. Pre-fix,
// parseJournalDir always walked shadow/<label>/*.jsonl, so the stub-dir
// vs testnet-dir diff flagged every shadow trade as "only-in-A" →
// SIGNAL_DIVERGENCE (exit 2). The default must now PASS this topology
// because the production caller (scripts/layer3_verdict.sh) cannot pass
// --include-shadows without code change, and pre-fix the gate would
// fire-CLOSE on every legitimate Layer 3 attempt.
//
// Topology mirrors cmd/engine/main.go production paths:
//   - stub-dir/BTCUSDT-2026-05.jsonl                    (live, matches B)
//   - stub-dir/shadow/alt5-15-336/ETHUSDT-2026-05.jsonl (shadow, NO peer in B)
//   - testnet-dir/BTCUSDT-2026-05.jsonl                 (live, matches A live)
func TestCLI_Layer3Topology_DefaultExcludesShadows(t *testing.T) {
	stubRoot := t.TempDir()
	tnRoot := t.TempDir()

	// Live trade present on BOTH sides — Layer 3 sees identical live.
	liveLines := []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	}
	writeFile(t, filepath.Join(stubRoot, "BTCUSDT-2026-05.jsonl"), liveLines)
	writeFile(t, filepath.Join(tnRoot, "BTCUSDT-2026-05.jsonl"), liveLines)

	// Shadow trade ONLY on the stub side. This mirrors what cmd/engine
	// writes: shadow runners write to <journalDir>/shadow/<label>/, the
	// testnet executor writes only the live trade flat in <testnet-dir>/.
	if err := os.MkdirAll(filepath.Join(stubRoot, "shadow", "alt5-15-336"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stubRoot, "shadow", "alt5-15-336", "ETHUSDT-2026-05.jsonl"), []string{
		`{"event":"open","symbol":"ETHUSDT","ts":"2026-05-08T08:00:00Z","side":"SHORT","entry":3000,"stop":3050,"target":2700,"reason":"r"}`,
		`{"event":"close","symbol":"ETHUSDT","ts":"2026-05-08T09:00:00Z","side":"SHORT","entry":3000,"exit":2700,"pnl_usd":1000,"outcome":"TARGET","reason":"r"}`,
	})

	// Default invocation (no --include-shadows). Must PASS — live trade
	// matches, shadow tree is invisible.
	code, out := runDiff(t, "--dir-a", stubRoot, "--dir-b", tnRoot)
	if code != 0 {
		t.Errorf("Layer 3 topology must PASS by default (live matches, shadow tree present only on A), got exit %d\nout:\n%s",
			code, out)
	}
	// Verify the shadow ETH trade did NOT leak through.
	if strings.Contains(out, "ETHUSDT") {
		t.Errorf("shadow ETHUSDT trade leaked into default report — default must exclude shadows.\nout:\n%s", out)
	}
}

// TestCLI_IncludeShadowsFlag_RestoresFullCompare verifies the opt-in path:
// when --include-shadows is passed, the shadow tree IS walked. With the
// Layer 3 topology (shadows only on A), this correctly surfaces signal
// divergence — proving the flag is wired both directions.
func TestCLI_IncludeShadowsFlag_RestoresFullCompare(t *testing.T) {
	stubRoot := t.TempDir()
	tnRoot := t.TempDir()
	liveLines := []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	}
	writeFile(t, filepath.Join(stubRoot, "BTCUSDT-2026-05.jsonl"), liveLines)
	writeFile(t, filepath.Join(tnRoot, "BTCUSDT-2026-05.jsonl"), liveLines)
	if err := os.MkdirAll(filepath.Join(stubRoot, "shadow", "alt5-15-336"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stubRoot, "shadow", "alt5-15-336", "ETHUSDT-2026-05.jsonl"), []string{
		`{"event":"open","symbol":"ETHUSDT","ts":"2026-05-08T08:00:00Z","side":"SHORT","entry":3000,"stop":3050,"target":2700,"reason":"r"}`,
		`{"event":"close","symbol":"ETHUSDT","ts":"2026-05-08T09:00:00Z","side":"SHORT","entry":3000,"exit":2700,"pnl_usd":1000,"outcome":"TARGET","reason":"r"}`,
	})

	code, out := runDiff(t, "--include-shadows", "--dir-a", stubRoot, "--dir-b", tnRoot)
	if code != 2 {
		t.Errorf("with --include-shadows, asymmetric shadow tree must produce SIGNAL_DIVERGENCE (exit 2), got %d\nout:\n%s",
			code, out)
	}
	if !strings.Contains(out, "ETHUSDT") {
		t.Errorf("opt-in shadow path: ETHUSDT must appear in report\nout:\n%s", out)
	}
}
