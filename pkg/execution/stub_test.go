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

func TestShortSide_FeeSlippageMath(t *testing.T) {
	// Mirror of TestFeeAndSlippageMath but for Short. Setup:
	//   stake=$1000, entry=50000, stop=50100 (stop_dist=100), target=49500 (5R win).
	//   units = 1000/100 = 10. Notional = 10*50000 = $500k.
	//   FeeBps=8 → fee = $400. StopSlippageBps=5 → slip = $250 on losers only.
	t.Run("short_win_pays_fee_only", func(t *testing.T) {
		stub := &Stub{
			StakeUSDT:       1000,
			ExactFills:      true,
			FeeBps:          8,
			StopSlippageBps: 5,
		}
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: models.Short,
			EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
			Timestamp: time.Now().UTC(),
		})
		// Short target = 49500. Tick at 49500 (or below) triggers TARGET.
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 49500})

		if len(stub.results) != 1 {
			t.Fatalf("expected 1 trade, got %d", len(stub.results))
		}
		r := stub.results[0]
		if !r.won {
			t.Fatal("expected win")
		}
		// Short gross = entry - exit = 50000 - 49500 = 500 pts; units * pts = 10 * 500 = 5000.
		if r.grossUSDT != 5000 {
			t.Errorf("gross: want 5000, got %v", r.grossUSDT)
		}
		if r.feeUSDT != 400 {
			t.Errorf("fee: want 400, got %v", r.feeUSDT)
		}
		if r.slipUSDT != 0 {
			t.Errorf("slip: want 0 (winner), got %v", r.slipUSDT)
		}
		if r.pnlUSDT != 4600 {
			t.Errorf("net: want 4600, got %v", r.pnlUSDT)
		}
	})

	t.Run("short_loss_pays_fee_and_slippage", func(t *testing.T) {
		stub := &Stub{
			StakeUSDT:       1000,
			ExactFills:      true,
			FeeBps:          8,
			StopSlippageBps: 5,
		}
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: models.Short,
			EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
			Timestamp: time.Now().UTC(),
		})
		// Short stop = 50100. Tick at 50100 triggers STOP.
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 50100})

		r := stub.results[0]
		if r.won {
			t.Fatal("expected loss")
		}
		// Short gross = 50000 - 50100 = -100 pts; 10 * -100 = -1000.
		if r.grossUSDT != -1000 {
			t.Errorf("gross: want -1000, got %v", r.grossUSDT)
		}
		if r.feeUSDT != 400 || r.slipUSDT != 250 {
			t.Errorf("costs: want fee=400 slip=250, got fee=%v slip=%v", r.feeUSDT, r.slipUSDT)
		}
		if r.pnlUSDT != -1650 {
			t.Errorf("net: want -1650, got %v", r.pnlUSDT)
		}
	})
}

func TestFundingCostAccruesOverHoldTime(t *testing.T) {
	// FundingBpsPerDay accrues per-day fraction × notional. Setup:
	//   stake=$1000, entry=100, stop=99 (stop_dist=1), target=110 → win.
	//   units = 1000. Notional = $100,000.
	//   FundingBpsPerDay = 30 (0.30%/day). Hold = 12 hours = 0.5 days.
	//   Expected funding = 30/10000 * 100000 * 0.5 = $150.
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		FundingBpsPerDay: 30,
	}
	openTime := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})
	stub.OnTick(models.Tick{
		Symbol:    "X",
		Timestamp: openTime.Add(12 * time.Hour),
		Price:     110,
	})

	r := stub.results[0]
	if !r.won {
		t.Fatal("expected win")
	}
	// Gross = 1000 * (110-100) = 10000.
	if r.grossUSDT != 10000 {
		t.Errorf("gross: want 10000, got %v", r.grossUSDT)
	}
	if r.fundingUSDT != 150 {
		t.Errorf("funding: want 150 (30bp/day × 0.5 days × $100k notional), got %v", r.fundingUSDT)
	}
	if r.pnlUSDT != 10000-150 {
		t.Errorf("net: want %v, got %v", 10000-150, r.pnlUSDT)
	}
}

func TestSummaryAggregation_MixedTrades(t *testing.T) {
	// Run 4 trades through the same Stub: long-win, long-loss, short-win, short-loss.
	// Verify that s.results aggregates as Summary expects.
	//   stake=$1000, FeeBps=10, StopSlippageBps=5.
	//   Long: entry=100, stop=99, target=110.
	//     units=1000, notional=$100k. Fee=$100/trade. Slip=$50 on losers.
	//     Win: gross=+10000, net=10000-100 = 9900.
	//     Loss: gross=-1000, net=-1000-100-50 = -1150.
	//   Short: entry=100, stop=101, target=90.
	//     units=1000, notional=$100k. Fee=$100/trade. Slip=$50 on losers.
	//     Win: gross=+10000, net=9900.
	//     Loss: gross=-1000, net=-1150.
	stub := &Stub{
		StakeUSDT:       1000,
		ExactFills:      true,
		FeeBps:          10,
		StopSlippageBps: 5,
	}
	now := time.Now().UTC()

	// Helper to run one trade.
	runTrade := func(side models.Direction, entry, stop, target, exit float64) {
		stub.OnSignal(&models.Signal{
			Symbol: "X", Side: side,
			EntryPrice: entry, StopLoss: stop, TakeProfit: target,
			Timestamp: now,
		})
		stub.OnTick(models.Tick{Symbol: "X", Timestamp: now.Add(time.Hour), Price: exit})
	}

	runTrade(models.Long, 100, 99, 110, 110)   // long win
	runTrade(models.Long, 100, 99, 110, 99)    // long loss
	runTrade(models.Short, 100, 101, 90, 90)   // short win
	runTrade(models.Short, 100, 101, 90, 101)  // short loss

	if len(stub.results) != 4 {
		t.Fatalf("expected 4 trades, got %d", len(stub.results))
	}

	// Aggregate the way Summary does.
	var (
		wins, longCount, shortCount, longWins, shortWins int
		totalUSD, longNetUSD, shortNetUSD                float64
	)
	for _, r := range stub.results {
		totalUSD += r.pnlUSDT
		if r.won {
			wins++
		}
		if r.signal.Side == models.Long {
			longCount++
			longNetUSD += r.pnlUSDT
			if r.won {
				longWins++
			}
		} else {
			shortCount++
			shortNetUSD += r.pnlUSDT
			if r.won {
				shortWins++
			}
		}
	}

	// Each winner: net = 10000 - 100 = 9900. Each loser: net = -1000 - 100 - 50 = -1150.
	// Total = 2*9900 + 2*-1150 = 19800 - 2300 = 17500.
	if totalUSD != 17500 {
		t.Errorf("total net: got %v want 17500", totalUSD)
	}
	if longNetUSD != 9900-1150 {
		t.Errorf("long net: got %v want %v", longNetUSD, 9900-1150)
	}
	if shortNetUSD != 9900-1150 {
		t.Errorf("short net: got %v want %v", shortNetUSD, 9900-1150)
	}
	if wins != 2 {
		t.Errorf("wins: got %v want 2", wins)
	}
	if longCount != 2 || shortCount != 2 {
		t.Errorf("counts: long=%v short=%v, want 2 each", longCount, shortCount)
	}
	if longWins != 1 || shortWins != 1 {
		t.Errorf("per-side wins: long=%v short=%v, want 1 each", longWins, shortWins)
	}
	winRate := float64(wins) / float64(len(stub.results)) * 100
	if winRate != 50.0 {
		t.Errorf("WR: got %v want 50.0", winRate)
	}
}

func TestMaxHoldForceClose(t *testing.T) {
	// MaxHoldHours=1: position is force-closed after 1h.
	// Setup: long, entry=100, stop=99, target=110.
	// Tick at 105 after 2h: neither stop nor target hit, but past max-hold → force-close at 105.
	// Classification: won = (exitPrice > entryPrice) for Long → 105 > 100 → won=true.
	stub := &Stub{
		StakeUSDT:    1000,
		ExactFills:   true,
		MaxHoldHours: 1.0,
	}
	openTime := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})
	stub.OnTick(models.Tick{
		Symbol:    "X",
		Timestamp: openTime.Add(2 * time.Hour),
		Price:     105,
	})

	if len(stub.results) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(stub.results))
	}
	r := stub.results[0]
	if r.exitPrice != 105 {
		t.Errorf("exitPrice: got %v want 105 (force-close at tick price)", r.exitPrice)
	}
	if !r.won {
		t.Errorf("won: got false; long with exit > entry should be classified as win")
	}
	// Gross = 1000 units * (105-100) = 5000.
	if r.grossUSDT != 5000 {
		t.Errorf("gross: got %v want 5000", r.grossUSDT)
	}
	if stub.timeStopCount != 1 {
		t.Errorf("timeStopCount: got %v want 1", stub.timeStopCount)
	}
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
