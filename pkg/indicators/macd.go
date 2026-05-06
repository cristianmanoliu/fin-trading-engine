package indicators

// MACD is the Moving Average Convergence Divergence indicator (Appel, 1979).
// Composed of:
//   - MACD line  = EMA(fast) - EMA(slow)
//   - Signal line = EMA(signal_period) of MACD line
//   - Histogram  = MACD - Signal
//
// Standard parameters: fast=12, slow=26, signal=9.
//
// Trading interpretation:
//   - MACD crossing ABOVE Signal: bullish (buy)
//   - MACD crossing BELOW Signal: bearish (sell / short)
//   - Histogram width = momentum strength
//
// Not goroutine-safe — owned exclusively by the strategy runner goroutine.
// Mirrors EMA/RSI lifecycle: Primed() reports readiness; Value() returns
// (macd_line, signal_line, histogram).
type MACD struct {
	emaFast   *EMA
	emaSlow   *EMA
	emaSignal *EMA
}

// NewMACD creates a MACD with the given periods. Defaults if any param ≤ 0:
// fast=12, slow=26, signal=9.
func NewMACD(fast, slow, signal int) *MACD {
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	return &MACD{
		emaFast:   NewEMA(fast),
		emaSlow:   NewEMA(slow),
		emaSignal: NewEMA(signal),
	}
}

// Update adds a new price sample. The signal line begins updating once both
// fast and slow EMAs are primed (slow primes last).
func (m *MACD) Update(price float64) {
	m.emaFast.Update(price)
	m.emaSlow.Update(price)
	if !m.emaSlow.Primed() {
		return
	}
	macdLine := m.emaFast.Value() - m.emaSlow.Value()
	m.emaSignal.Update(macdLine)
}

// Value returns (macd_line, signal_line). Returns (0, 0) if not yet primed.
// Caller can compute histogram = macd_line - signal_line.
func (m *MACD) Value() (macd, signal float64) {
	if !m.Primed() {
		return 0, 0
	}
	return m.emaFast.Value() - m.emaSlow.Value(), m.emaSignal.Value()
}

// Primed reports true once both EMAs and the signal-line EMA are primed.
// Until then, signal-line crossover detection isn't reliable.
func (m *MACD) Primed() bool {
	return m.emaSlow.Primed() && m.emaSignal.Primed()
}
