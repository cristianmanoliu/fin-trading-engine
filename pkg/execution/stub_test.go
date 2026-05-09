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

func TestMaxHoldForceClose_AppliesSlipOnWinner(t *testing.T) {
	// Bug 1 (CLAUDE.md): max-hold force-close is a market exit, so slippage
	// should be charged regardless of pnl direction. Pre-fix, winner
	// time-stops (won=true) skipped slippage, under-modeling the realized
	// cost of forced market exits.
	//
	// Setup: long, entry=100, stop=99, target=110, MaxHold=1h.
	// Tick at 105 after 2h: time-stop fires, in profit (won=true).
	// Notional at trade open: stake/stop_dist × entry = 1000/1 × 100 = $100k.
	// Round-trip fee 10bp on 100k = $100. Slip 5bp on 100k = $50.
	// Gross = 1000 × (105-100) = $5,000.
	// Net (post-fix) = 5000 - 100 - 50 = $4,850.
	stub := &Stub{
		StakeUSDT:       1000,
		ExactFills:      true,
		MaxHoldHours:    1.0,
		FeeBps:          10,
		StopSlippageBps: 5,
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
	if !r.won {
		t.Fatal("setup: should be classified as winner (105 > 100)")
	}
	if r.feeUSDT != 100 {
		t.Errorf("feeUSDT: got %v want 100 (10bp on $100k notional)", r.feeUSDT)
	}
	if r.slipUSDT != 50 {
		t.Errorf("slipUSDT: got %v want 50 (5bp on $100k notional, market exit)", r.slipUSDT)
	}
	if r.pnlUSDT != 5000-100-50 {
		t.Errorf("pnlUSDT: got %v want %v (gross-fee-slip)", r.pnlUSDT, 5000-100-50)
	}
}

func TestMaxHoldForceClose_AppliesSlipOnLoser(t *testing.T) {
	// Loser time-stops already applied slip (won=false → slip path).
	// Regression-guard so the fix doesn't accidentally drop slip when
	// won=false.
	stub := &Stub{
		StakeUSDT:       1000,
		ExactFills:      true,
		MaxHoldHours:    1.0,
		FeeBps:          10,
		StopSlippageBps: 5,
	}
	openTime := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})
	// Tick at 99.5 after 2h: above stop, but past max-hold. Force-close at 99.5,
	// in loss (won=false).
	stub.OnTick(models.Tick{
		Symbol:    "X",
		Timestamp: openTime.Add(2 * time.Hour),
		Price:     99.5,
	})
	r := stub.results[0]
	if r.won {
		t.Fatal("setup: should be classified as loser (99.5 < 100)")
	}
	if r.slipUSDT != 50 {
		t.Errorf("slipUSDT: got %v want 50 (loser slip preserved)", r.slipUSDT)
	}
}

func TestTrailingStop_RatchetsToBE_AndExitsProfitably(t *testing.T) {
	// B1 trailing stop. Setup: long, entry=100, stop=99 (1R=$1), target=110 (10R), trail interval=1R.
	// Tick 1 at 102 (2R favorable): stop should ratchet from 99 to entry+1R = 101.
	// Tick 2 at 100.9 (back below 101): hits ratcheted stop → exit at 100.9 (or 101 with ExactFills).
	// won = true (exit > entry under TrailingStopMode classification).
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		TrailingStopMode: true,
		TrailIntervalR:   1.0,
	}
	openTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})

	// Tick 1: 2R favorable. Should ratchet stop to entry + 1R = 101.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(time.Hour), Price: 102})
	if stub.position == nil {
		t.Fatal("position closed too early on first tick")
	}
	if stub.position.Signal.StopLoss != 101 {
		t.Errorf("trailing stop did not ratchet: want 101, got %v", stub.position.Signal.StopLoss)
	}
	if stub.position.MaxFavorableR != 2.0 {
		t.Errorf("MaxFavorableR: want 2.0, got %v", stub.position.MaxFavorableR)
	}

	// Tick 2: pullback to 100.9. Hits the ratcheted stop (101) — exit at exact stop.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(2 * time.Hour), Price: 100.9})
	if len(stub.results) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(stub.results))
	}
	r := stub.results[0]
	if r.exitPrice != 101 {
		t.Errorf("exit price: want 101 (ExactFills at ratcheted stop), got %v", r.exitPrice)
	}
	if !r.won {
		t.Errorf("trailed stop above entry should classify as WON, got loss")
	}
	// Gross = (101 - 100) × (1000 / 1) = 1000.
	if r.grossUSDT != 1000 {
		t.Errorf("gross: want 1000, got %v", r.grossUSDT)
	}
}

func TestTrailingStop_AppliesSlipOnWinner(t *testing.T) {
	// Generalization of Bug 1 to the trailing-stop winner path.
	// Same setup as TestTrailingStop_RatchetsToBE_AndExitsProfitably:
	// trailing stop ratchets to BE+1R, exits profitably (won=true).
	// Pre-fix: !won-gated slip skipped slippage on trailing winners
	// despite the exit being a market stop. Post-fix: slip applies.
	//
	// Notional = stake/stop_dist × entry = 1000/1 × 100 = $100k.
	// Fee 10bp = $100 round-trip. Slip 5bp = $50.
	// Gross = (101-100) × 1000 = $1,000. Net = 1000 - 100 - 50 = $850.
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		TrailingStopMode: true,
		TrailIntervalR:   1.0,
		FeeBps:           10,
		StopSlippageBps:  5,
	}
	openTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(time.Hour), Price: 102})  // ratchets
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(2 * time.Hour), Price: 100.9}) // exits at 101

	r := stub.results[0]
	if !r.won {
		t.Fatal("setup: trailing stop above entry should classify as won")
	}
	if r.slipUSDT != 50 {
		t.Errorf("slipUSDT: got %v want 50 (5bp on $100k notional, market exit on stop)", r.slipUSDT)
	}
	if r.feeUSDT != 100 {
		t.Errorf("feeUSDT: got %v want 100 (10bp on $100k notional)", r.feeUSDT)
	}
	if r.pnlUSDT != 1000-100-50 {
		t.Errorf("pnlUSDT: got %v want %v (gross-fee-slip)", r.pnlUSDT, 1000-100-50)
	}
}

func TestTrailingStop_NoRatchetBeforeOneR(t *testing.T) {
	// B1: until favorable move ≥ 1R, stop stays at original.
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		TrailingStopMode: true,
		TrailIntervalR:   1.0,
	}
	openTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Short,
		EntryPrice: 100, StopLoss: 101, TakeProfit: 90,
		Timestamp: openTime,
	})

	// Tick at 99.5 = 0.5R favorable for short. Below the 1R threshold → no ratchet.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(time.Hour), Price: 99.5})
	if stub.position == nil {
		t.Fatal("position closed unexpectedly")
	}
	if stub.position.Signal.StopLoss != 101 {
		t.Errorf("stop ratcheted prematurely: want 101 (original), got %v", stub.position.Signal.StopLoss)
	}

	// Tick at 99 = 1R favorable. Should ratchet to entry (BE) — locked at 0R.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(2 * time.Hour), Price: 99})
	if stub.position.Signal.StopLoss != 100 {
		t.Errorf("at 1R favorable, stop should ratchet to entry (100), got %v", stub.position.Signal.StopLoss)
	}
}

func TestMultiLevelTP_PartialAtMidR_ThenFullWinAt6R(t *testing.T) {
	// B2: long, entry=100, stop=99 (1R=$1), target=106 (6R), mid-R=3, mid-frac=0.5.
	// Tick 1 at 103 (3R) → partial close 50% at 103. Stop raises to 100 (BE). RemainingFrac=0.5.
	// Tick 2 at 106 (6R) → final TARGET on remaining 50%.
	// Expected results: 2 tradeResults.
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		MultiLevelTPMode: true,
		MidRMult:         3.0,
		MidFrac:          0.5,
	}
	openTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 106,
		Timestamp: openTime,
	})

	// Mid-R hit: partial close.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(time.Hour), Price: 103})
	if len(stub.results) != 1 {
		t.Fatalf("expected 1 partial result after mid-R hit, got %d", len(stub.results))
	}
	if !stub.position.MidRHit {
		t.Errorf("MidRHit not set after partial")
	}
	if stub.position.RemainingFrac != 0.5 {
		t.Errorf("RemainingFrac: want 0.5, got %v", stub.position.RemainingFrac)
	}
	if stub.position.Signal.StopLoss != 100 {
		t.Errorf("stop should raise to BE (100), got %v", stub.position.Signal.StopLoss)
	}
	partial := stub.results[0]
	// Partial: units = 1000 × 0.5 / 1 = 500. gross = 500 × (103-100) = 1500.
	if partial.grossUSDT != 1500 {
		t.Errorf("partial gross: want 1500, got %v", partial.grossUSDT)
	}
	if !partial.won {
		t.Errorf("partial close should be classified as won")
	}

	// 6R hit: final target on remainder.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(2 * time.Hour), Price: 106})
	if len(stub.results) != 2 {
		t.Fatalf("expected 2 results after final close, got %d", len(stub.results))
	}
	final := stub.results[1]
	// Remainder: units = 1000 × 0.5 / 1 = 500. gross = 500 × (106-100) = 3000.
	if final.grossUSDT != 3000 {
		t.Errorf("final gross: want 3000, got %v", final.grossUSDT)
	}
	if !final.won {
		t.Errorf("final 6R should be won")
	}
	if stub.partialCloseCount != 1 {
		t.Errorf("partialCloseCount: want 1, got %v", stub.partialCloseCount)
	}
}

func TestMultiLevelTP_PartialAtMidR_ThenBEStop(t *testing.T) {
	// B2: short, entry=100, stop=101, target=94 (6R), mid-R=3.
	// Mid-R = 100 - 3 = 97. Tick 1 at 97 → partial close 50%. Stop raises to 100 (BE).
	// Tick 2 at 100 → BE stop fires on remainder. won = true (gross = 0 means classify by sign:
	//   exitPrice > entryPrice for long, < for short. 100 < 100 is false → won = false).
	stub := &Stub{
		StakeUSDT:        1000,
		ExactFills:       true,
		MultiLevelTPMode: true,
		MidRMult:         3.0,
		MidFrac:          0.5,
	}
	openTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Short,
		EntryPrice: 100, StopLoss: 101, TakeProfit: 94,
		Timestamp: openTime,
	})

	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(time.Hour), Price: 97})
	if !stub.position.MidRHit {
		t.Fatal("mid-R should fire at 97 for short with entry=100, stop=101")
	}
	// Stop raises to BE (entry = 100).
	if stub.position.Signal.StopLoss != 100 {
		t.Errorf("BE stop: want 100, got %v", stub.position.Signal.StopLoss)
	}

	// Tick 2 back at 100: hits BE-stop on remainder.
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: openTime.Add(2 * time.Hour), Price: 100})
	if len(stub.results) != 2 {
		t.Fatalf("want 2 results (partial + BE-stop), got %d", len(stub.results))
	}
	final := stub.results[1]
	// Remainder gross = 500 × (100-100) = 0.
	if final.grossUSDT != 0 {
		t.Errorf("BE-stop gross: want 0, got %v", final.grossUSDT)
	}
	// Classification: TrailingStopMode is OFF but MultiLevelTPMode + MidRHit → use PnL sign.
	// Short BE: exitPrice (100) < entryPrice (100) is false → won = false. Correct.
	if final.won {
		t.Errorf("BE-stop hit at exact entry should classify as not won")
	}
}

func TestNoModeBaseline_PreservedExactly(t *testing.T) {
	// Regression: with TrailingStopMode and MultiLevelTPMode both off, the legacy
	// behavior is byte-exact. Same stake, fees, slip as TestFeeAndSlippageMath.
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
	r := stub.results[0]
	// Same expectations as the original fee/slip test.
	if r.grossUSDT != 5000 || r.feeUSDT != 400 || r.slipUSDT != 0 || r.pnlUSDT != 4600 {
		t.Errorf("baseline math drifted: gross=%v fee=%v slip=%v net=%v (want 5000/400/0/4600)",
			r.grossUSDT, r.feeUSDT, r.slipUSDT, r.pnlUSDT)
	}
}

func TestJournalCloseEventCostFields(t *testing.T) {
	// Close events should carry the cost decomposition (gross, fee, slip,
	// funding, notional) so forward_paper_status.sh can derive realized
	// fee bps and slip bps without re-deriving from pnl_usd.
	//
	// Setup mirrors TestFeeAndSlippageMath/loss_pays_fee_and_slippage:
	//   stake=$1000, entry=50000, stop=49900, target=50500. units=10.
	//   notional=$500k. FeeBps=8 → fee=$400. SlipBps=5 (loser) → slip=$250.
	dir := t.TempDir()
	stub := &Stub{
		StakeUSDT:       1000,
		ExactFills:      true,
		FeeBps:          8,
		StopSlippageBps: 5,
		JournalPath:     dir,
		Symbol:          "X",
	}
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: time.Now().UTC(),
	})
	stub.OnTick(models.Tick{Symbol: "X", Timestamp: time.Now().UTC(), Price: 49900})

	// Find the close event in the journal.
	matches, err := filepath.Glob(filepath.Join(dir, "X-*.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatal("no journal file created")
	}
	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var closeLine map[string]any
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("invalid JSON: %s", sc.Text())
		}
		if m["event"] == "close" {
			closeLine = m
		}
	}
	if closeLine == nil {
		t.Fatal("no close event found")
	}

	// Numeric assertions. JSON unmarshals numbers as float64 — match exactly.
	check := func(field string, want float64) {
		got, ok := closeLine[field].(float64)
		if !ok {
			t.Errorf("field %s missing or not numeric: %v", field, closeLine[field])
			return
		}
		if got != want {
			t.Errorf("field %s: want %v, got %v", field, want, got)
		}
	}
	check("gross_usd", -1000)
	check("fee_usd", 400)
	check("slip_usd", 250)
	check("notional_usd", 500000)
	// funding_usd should be omitted (0) since neither provider nor const-rate set.
	if v, present := closeLine["funding_usd"]; present && v.(float64) != 0 {
		t.Errorf("funding_usd should be 0/omitted, got %v", v)
	}
}

// ── RecoverFromJournal tests ──────────────────────────────────────────────

// writeJournal writes the given JSON-serialized lines to <dir>/<symbol>-<month>.jsonl.
// month should be "YYYY-MM" matching what the recovery scans.
func writeJournal(t *testing.T, dir, symbol, month string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, symbol+"-"+month+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func currentMonthStr() string  { return time.Now().UTC().Format("2006-01") }
func priorMonthStr() string    { return time.Now().UTC().AddDate(0, -1, 0).Format("2006-01") }

func TestRecover_NoJournalDir_NoOp(t *testing.T) {
	stub := &Stub{StakeUSDT: 1000, Symbol: "X"} // JournalPath empty
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recovered {
		t.Errorf("expected no recovery without JournalPath, got recovered=true")
	}
}

func TestRecover_EmptyDir_NoOp(t *testing.T) {
	dir := t.TempDir()
	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "BTCUSDT"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recovered {
		t.Errorf("expected no recovery on empty dir, got recovered=true")
	}
}

func TestRecover_OpenWithoutClose_Recovers(t *testing.T) {
	dir := t.TempDir()
	openLine := `{"event":"open","symbol":"BTCUSDT","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"test"}`
	writeJournal(t, dir, "BTCUSDT", currentMonthStr(), []string{openLine})

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "BTCUSDT"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !recovered {
		t.Fatalf("expected recovery for open without close")
	}
	if stub.position == nil {
		t.Fatal("position not set after recovery")
	}
	sig := stub.position.Signal
	if sig.Side != models.Long {
		t.Errorf("side: want LONG, got %v", sig.Side)
	}
	if sig.EntryPrice != 50000 || sig.StopLoss != 49500 || sig.TakeProfit != 53000 {
		t.Errorf("price fields wrong: entry=%v stop=%v target=%v", sig.EntryPrice, sig.StopLoss, sig.TakeProfit)
	}
	expectedTS, _ := time.Parse(time.RFC3339, "2026-05-07T08:00:00Z")
	if !sig.Timestamp.Equal(expectedTS) {
		t.Errorf("ts: want %v, got %v", expectedTS, sig.Timestamp)
	}
	if stub.position.OriginalStopDist != 500 {
		t.Errorf("OriginalStopDist: want 500, got %v", stub.position.OriginalStopDist)
	}
	if stub.position.MidRHit || stub.position.RemainingFrac != 1.0 {
		t.Errorf("expected fresh-position state for non-partial recovery, got MidRHit=%v RemFrac=%v",
			stub.position.MidRHit, stub.position.RemainingFrac)
	}
}

func TestRecover_OpenAndClose_NoRecovery(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"test"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-07T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"stop":49500,"target":53000,"pnl_pts":3000,"pnl_usd":6000,"outcome":"TARGET","reason":"test"}`,
	}
	writeJournal(t, dir, "BTCUSDT", currentMonthStr(), lines)

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "BTCUSDT"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recovered || stub.position != nil {
		t.Errorf("expected no recovery for closed trade")
	}
}

func TestRecover_OpenAndPartialClose_RecoversWithMidRHit(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"test"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-07T09:00:00Z","side":"LONG","entry":100,"exit":103,"stop":99,"target":106,"pnl_pts":3,"pnl_usd":1500,"outcome":"PARTIAL","reason":"test"}`,
	}
	writeJournal(t, dir, "X", currentMonthStr(), lines)

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovery after partial close (remainder still open)")
	}
	if !stub.position.MidRHit {
		t.Error("expected MidRHit=true after recovering partial-closed open")
	}
	if stub.position.RemainingFrac != 0.5 {
		t.Errorf("RemainingFrac: want 0.5, got %v", stub.position.RemainingFrac)
	}
}

func TestRecover_OpenPartialAndFullClose_NoRecovery(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"test"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-07T09:00:00Z","side":"LONG","entry":100,"exit":103,"stop":99,"target":106,"outcome":"PARTIAL","reason":"test"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-07T10:00:00Z","side":"LONG","entry":100,"exit":106,"stop":100,"target":106,"outcome":"TARGET","reason":"test"}`,
	}
	writeJournal(t, dir, "X", currentMonthStr(), lines)

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	recovered, _ := stub.RecoverFromJournal()
	if recovered || stub.position != nil {
		t.Errorf("expected no recovery — partial then full target should clear position")
	}
}

func TestRecover_TwoOpensSecondNotClosed_RecoversSecond(t *testing.T) {
	// Defensive: legitimate sequence is open→close→open→close. Two opens with
	// only one close (and the second un-closed) shouldn't normally happen
	// (OnSignal rejects when position is already open) but recovery must
	// still pick the LATEST unclosed open.
	dir := t.TempDir()
	lines := []string{
		`{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"first"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-07T09:00:00Z","side":"LONG","entry":100,"exit":99,"outcome":"STOP","reason":"first"}`,
		`{"event":"open","symbol":"X","ts":"2026-05-07T10:00:00Z","side":"SHORT","entry":200,"stop":210,"target":140,"reason":"second"}`,
	}
	writeJournal(t, dir, "X", currentMonthStr(), lines)

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	recovered, _ := stub.RecoverFromJournal()
	if !recovered {
		t.Fatal("expected recovery of second open")
	}
	if stub.position.Signal.Side != models.Short || stub.position.Signal.EntryPrice != 200 {
		t.Errorf("recovered wrong position: side=%v entry=%v",
			stub.position.Signal.Side, stub.position.Signal.EntryPrice)
	}
}

func TestRecover_PriorMonthOpen_CrossMonthRecovery(t *testing.T) {
	// Position opened in prior month's journal, no close anywhere — should
	// recover by reading prior-month file.
	dir := t.TempDir()
	openLine := `{"event":"open","symbol":"X","ts":"2026-04-25T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"crossmonth"}`
	writeJournal(t, dir, "X", priorMonthStr(), []string{openLine})

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovery from prior month's journal")
	}
}

func TestRecover_CorruptTrailingLine_StillRecovers(t *testing.T) {
	dir := t.TempDir()
	// Open event followed by a corrupt partial line (e.g., engine killed mid-write).
	path := filepath.Join(dir, "X-"+currentMonthStr()+".jsonl")
	f, _ := os.Create(path)
	f.WriteString(`{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"test"}` + "\n")
	f.WriteString(`{"event":"clo`) // truncated
	f.Close()

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("expected corrupt-line tolerance, got err: %v", err)
	}
	if !recovered {
		t.Fatal("corrupt trailing line should not block recovery of valid open")
	}
}

func TestRecover_PreExistingPosition_NoOverwrite(t *testing.T) {
	dir := t.TempDir()
	openLine := `{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"journal"}`
	writeJournal(t, dir, "X", currentMonthStr(), []string{openLine})

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X"}
	// Plant an existing position (simulate already-active).
	stub.position = &OpenPosition{
		Signal: &models.Signal{
			Symbol: "X", Side: models.Short, EntryPrice: 999,
			StopLoss: 1100, TakeProfit: 500, Timestamp: time.Now(),
		},
		OriginalStopDist: 101, RemainingFrac: 1.0,
	}

	recovered, err := stub.RecoverFromJournal()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if recovered {
		t.Errorf("must not overwrite existing position")
	}
	if stub.position.Signal.EntryPrice != 999 {
		t.Errorf("position was overwritten: entry=%v", stub.position.Signal.EntryPrice)
	}
}

func TestRecover_PreOpenTickGuard_DoesNotCloseRecovered(t *testing.T) {
	// After recovery, a tick with timestamp BEFORE the position's ts should
	// be skipped — even if its price would otherwise hit stop/target.
	dir := t.TempDir()
	openLine := `{"event":"open","symbol":"X","ts":"2026-05-07T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":110,"reason":"test"}`
	writeJournal(t, dir, "X", currentMonthStr(), []string{openLine})

	stub := &Stub{StakeUSDT: 1000, JournalPath: dir, Symbol: "X", ExactFills: true}
	if recovered, _ := stub.RecoverFromJournal(); !recovered {
		t.Fatal("recovery should have succeeded")
	}

	// Pre-open tick at a price that would hit target — must be ignored.
	stub.OnTick(models.Tick{
		Symbol: "X",
		Timestamp: time.Date(2026, 5, 7, 7, 0, 0, 0, time.UTC), // 1h BEFORE open
		Price: 110,
	})
	if stub.position == nil {
		t.Fatal("pre-open tick at target price closed the recovered position — guard failed")
	}

	// Post-open tick at target price — should close normally.
	stub.OnTick(models.Tick{
		Symbol: "X",
		Timestamp: time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC), // 1h AFTER open
		Price: 110,
	})
	if stub.position != nil {
		t.Fatal("post-open tick at target price should have closed the recovered position")
	}
	if len(stub.results) != 1 || !stub.results[0].won {
		t.Errorf("expected one winning trade after recovery+target, got %d results", len(stub.results))
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

func TestSummary_NoTrades_NoCrash(t *testing.T) {
	// Empty Stub — no signals, no ticks. Summary must not panic and must
	// leave state untouched. Backtest configs that produce zero signals
	// (rare strategies, sparse data) hit this path.
	stub := &Stub{StakeUSDT: 1000}
	stub.Summary()
	if len(stub.results) != 0 {
		t.Errorf("empty Summary should leave results empty, got %d", len(stub.results))
	}
	if stub.position != nil {
		t.Errorf("empty Summary should leave position nil, got %+v", stub.position)
	}
}

func TestSummary_ForceClosesOpenPositionAsLoss(t *testing.T) {
	// Backtest data exhausts while a position is open. Summary force-closes
	// at LastPrice with won=false (conservative — don't claim wins that
	// didn't actually hit target). Slippage is applied because closePosition
	// treats won=false as a stop event.
	//
	// Regression-guards two things:
	//   (1) the force-close branch fires (s.position consumed → s.results grows)
	//   (2) the trade is recorded as a loss regardless of price direction
	stub := &Stub{
		StakeUSDT:       1000,
		ExactFills:      true,
		FeeBps:          10,
		StopSlippageBps: 5,
	}
	openTime := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	stub.OnSignal(&models.Signal{
		Symbol: "X", Side: models.Long,
		EntryPrice: 100, StopLoss: 99, TakeProfit: 110,
		Timestamp: openTime,
	})
	// Mid-trade tick at 105 — in profit, but neither stop nor target hit.
	stub.OnTick(models.Tick{
		Symbol: "X", Timestamp: openTime.Add(30 * time.Minute), Price: 105,
	})
	if stub.position == nil {
		t.Fatal("position should still be open before Summary")
	}

	stub.Summary()

	if stub.position != nil {
		t.Errorf("Summary should clear position, got %+v", stub.position)
	}
	if len(stub.results) != 1 {
		t.Fatalf("expected 1 result after Summary force-close, got %d", len(stub.results))
	}
	r := stub.results[0]
	if r.won {
		t.Error("force-closed-at-end-of-data should be recorded as loss (won=false)")
	}
	if r.exitPrice != 105 {
		t.Errorf("exitPrice: got %v want 105 (LastPrice at force-close)", r.exitPrice)
	}
	// Slippage applied because won=false. Notional = 1000/1 × 100 = 100k. Slip 5bp = $50.
	if r.slipUSDT != 50 {
		t.Errorf("slipUSDT: got %v want 50 (5bp on $100k notional)", r.slipUSDT)
	}
}
