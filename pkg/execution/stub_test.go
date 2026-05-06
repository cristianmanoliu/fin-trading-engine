package execution

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestJournalSink(t *testing.T) {
	dir := t.TempDir()

	stub := &Stub{
		StakeUSDT:   1000,
		JournalPath: dir,
		Symbol:      "BTCUSDT",
	}

	sig := &models.Signal{
		Symbol:     "BTCUSDT",
		Side:       models.Long,
		EntryPrice: 50000,
		StopLoss:   49000,
		TakeProfit: 52000,
		Timestamp:  time.Now().UTC(),
		Reason:     "absorption",
	}

	stub.OnSignal(sig)

	// Closing tick hits the target.
	stub.OnTick(models.Tick{
		Symbol:    "BTCUSDT",
		Timestamp: time.Now().UTC(),
		Price:     52001,
	})

	// Expect one JSONL file with exactly 2 lines.
	pattern := filepath.Join(dir, "BTCUSDT-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Fatal("no journal file created")
	}

	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var lines []map[string]any
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("invalid JSON line: %s", sc.Text())
		}
		lines = append(lines, m)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 journal lines, got %d", len(lines))
	}
	if lines[0]["event"] != "open" {
		t.Errorf("first line event: expected 'open', got %q", lines[0]["event"])
	}
	if lines[1]["event"] != "close" {
		t.Errorf("second line event: expected 'close', got %q", lines[1]["event"])
	}
	if lines[1]["outcome"] != "TARGET" {
		t.Errorf("expected outcome TARGET, got %q", lines[1]["outcome"])
	}
}

func TestFeeAndSlippageMath(t *testing.T) {
	// Setup: stake=$1000, entry=50000, stop=49900 (stop_dist=100, 0.2% of entry).
	// units = 1000/100 = 10. Notional = 10 * 50000 = $500k.
	// Win at exact target 50500 (5R = +500 pts).
	// Loss at exact stop 49900 (-1R = -100 pts).
	// FeeBps=8 → fee = 8/10000 * 500000 = $400 per trade.
	// StopSlippageBps=5 (losers only) → slip = 5/10000 * 500000 = $250.

	t.Run("win_pays_fee_only", func(t *testing.T) {
		stub := &Stub{
			StakeUSDT:       1000,
			ExactFills:      true,
			FeeBps:          8,
			StopSlippageBps: 5,
		}
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: models.Long,
			EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
			Timestamp: time.Now().UTC(),
		})
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 50500})

		if len(stub.results) != 1 {
			t.Fatalf("expected 1 trade, got %d", len(stub.results))
		}
		r := stub.results[0]
		if !r.won {
			t.Fatalf("expected win, got loss")
		}
		// Gross = 10 * 500 = $5000.
		if r.grossUSDT != 5000 {
			t.Errorf("gross: want 5000, got %v", r.grossUSDT)
		}
		// Fee = $400.
		if r.feeUSDT != 400 {
			t.Errorf("fee: want 400, got %v", r.feeUSDT)
		}
		// No slip on a winner.
		if r.slipUSDT != 0 {
			t.Errorf("slip: want 0, got %v", r.slipUSDT)
		}
		// Net = 5000 - 400 = 4600.
		if r.pnlUSDT != 4600 {
			t.Errorf("net pnl: want 4600, got %v", r.pnlUSDT)
		}
	})

	t.Run("loss_pays_fee_and_slippage", func(t *testing.T) {
		stub := &Stub{
			StakeUSDT:       1000,
			ExactFills:      true,
			FeeBps:          8,
			StopSlippageBps: 5,
		}
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: models.Long,
			EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
			Timestamp: time.Now().UTC(),
		})
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 49900})

		r := stub.results[0]
		if r.won {
			t.Fatalf("expected loss, got win")
		}
		// Gross = 10 * -100 = -$1000.
		if r.grossUSDT != -1000 {
			t.Errorf("gross: want -1000, got %v", r.grossUSDT)
		}
		// Fee = $400, slip = $250.
		if r.feeUSDT != 400 {
			t.Errorf("fee: want 400, got %v", r.feeUSDT)
		}
		if r.slipUSDT != 250 {
			t.Errorf("slip: want 250, got %v", r.slipUSDT)
		}
		// Net = -1000 - 400 - 250 = -1650.
		if r.pnlUSDT != -1650 {
			t.Errorf("net pnl: want -1650, got %v", r.pnlUSDT)
		}
	})

	t.Run("zero_costs_match_pre_fee_behavior", func(t *testing.T) {
		stub := &Stub{StakeUSDT: 1000, ExactFills: true} // FeeBps=0, no slip
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: models.Long,
			EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
			Timestamp: time.Now().UTC(),
		})
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 50500})

		r := stub.results[0]
		if r.feeUSDT != 0 || r.slipUSDT != 0 {
			t.Errorf("expected zero costs, got fee=%v slip=%v", r.feeUSDT, r.slipUSDT)
		}
		if r.pnlUSDT != r.grossUSDT {
			t.Errorf("net should equal gross when no costs; got net=%v gross=%v", r.pnlUSDT, r.grossUSDT)
		}
		if r.pnlUSDT != 5000 {
			t.Errorf("expected 5000 (5R win), got %v", r.pnlUSDT)
		}
	})
}

func TestJournalDisabledInBacktest(t *testing.T) {
	// When JournalPath is empty (backtest mode), no files should be created.
	stub := &Stub{StakeUSDT: 1000} // JournalPath intentionally not set

	sig := &models.Signal{
		Symbol:     "BTCUSDT",
		Side:       models.Short,
		EntryPrice: 50000,
		StopLoss:   51000,
		TakeProfit: 48000,
		Timestamp:  time.Now().UTC(),
		Reason:     "breakout",
	}
	stub.OnSignal(sig)
	stub.OnTick(models.Tick{Price: 47999}) // target hit

	// Should complete without panic and without creating any files.
	// Nothing to assert beyond "no crash".
}
