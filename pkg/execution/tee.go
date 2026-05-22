package execution

import "github.com/cristianmanoliu/fin-trading-engine/pkg/models"

// teeExec is the minimal Executor surface used by TeeExecutor. Defined
// locally rather than importing pkg/strategy.Executor because pkg/strategy
// depends on this package; importing back would create a cycle. Go uses
// structural typing for interfaces, so TeeExecutor still satisfies
// pkg/strategy.Executor at the call site without an explicit conversion.
type teeExec interface {
	OnSignal(sig *models.Signal)
	OnTick(tick models.Tick)
	Summary()
}

// TeeExecutor fans Executor calls to two underlying executors. Used for
// the Layer 3 shadow-parity gate per
// results/real_money_executor_architecture_decision_rule_2026-05-08.md
// "Layer 3 — Production shadow mode": run BinanceLive on testnet
// alongside the existing Stub on the same input ticks for ≥7 days, then
// diff the journals via cmd/journal_diff.
//
// Semantics:
//   - Calls fan out sequentially: Primary first, then Shadow. Sequential
//     ordering means Primary completes before Shadow runs, so a panic in
//     Shadow can't disturb Primary's already-completed work.
//   - No panic recovery — Layer 3's job is to validate that BinanceLive
//     behaves correctly under real testnet network/fill conditions; a
//     panic in the testnet executor is a real signal the operator must
//     see, not something to mask.
//   - Both executors maintain independent journals + recovery state.
//     They see identical OnSignal/OnTick streams (single-engine,
//     single-subscription) — exactly the "same input ticks" the locked
//     rule requires.
//
// Concurrency contract: matches whatever the underlying executors
// provide. Stub is synchronous; BinanceLive's OnSignal returns
// immediately (spawns a goroutine internally) and OnTick returns after
// minimal local work (also spawning a goroutine on close). Sequential
// tee adds at most one extra synchronous step per call — fast.
type TeeExecutor struct {
	Primary teeExec
	Shadow  teeExec
}

func (t *TeeExecutor) OnSignal(sig *models.Signal) {
	t.Primary.OnSignal(sig)
	t.Shadow.OnSignal(sig)
}

func (t *TeeExecutor) OnTick(tick models.Tick) {
	t.Primary.OnTick(tick)
	t.Shadow.OnTick(tick)
}

func (t *TeeExecutor) Summary() {
	t.Primary.Summary()
	t.Shadow.Summary()
}
