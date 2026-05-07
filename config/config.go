package config

import (
	"fmt"
	"os"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Symbol   string         `yaml:"symbol"`
	Exchange ExchangeConfig `yaml:"exchange"`
	Strategy StrategyConfig `yaml:"strategy"`
	Backtest BacktestConfig `yaml:"backtest"`
	Zones    []ZoneConfig   `yaml:"zones"`
}

type ExchangeConfig struct {
	WSURL   string `yaml:"ws_url"`
	RESTURL string `yaml:"rest_url"` // Binance Futures REST base URL; defaults to https://fapi.binance.com
}

type StrategyConfig struct {
	// ProximityPct is the fraction of price considered "near" a level (e.g. 0.001 = 0.1%).
	ProximityPct float64 `yaml:"proximity_pct"`
	// WickRatio is the minimum wick/body ratio required to flag absorption.
	WickRatio float64 `yaml:"wick_ratio"`
	// BreakoutBodyRatio is the minimum body/range ratio to confirm a breakout candle.
	BreakoutBodyRatio float64 `yaml:"breakout_body_ratio"`
	// AbsorptionCandles is how many consecutive candles must show absorption wicks.
	AbsorptionCandles int `yaml:"absorption_candles"`
	// StopBufferPct is the fraction added beyond the wick/level for stop placement.
	StopBufferPct float64 `yaml:"stop_buffer_pct"`
	// MinRR is the minimum reward/risk ratio required to take a trade. 0 disables the filter.
	MinRR float64 `yaml:"min_rr"`
	// TargetRR sets a fixed reward/risk multiplier for the take-profit. 0 = use VWAP as target.
	TargetRR float64 `yaml:"target_rr"`
	// MomentumMode switches entry logic: ignore key levels, enter only on 5m momentum candles
	// aligned with the 4H bias. TargetRR must be set. Absorptions and breakouts are disabled.
	MomentumMode bool `yaml:"momentum_mode"`
	// VWAPDeviationMode switches entry logic: fade price stretched ≥ VWAPDeviationPct from
	// the session VWAP. The reversal candle must close back toward VWAP. Target = VWAP.
	VWAPDeviationMode bool    `yaml:"vwap_deviation_mode"`
	VWAPDeviationPct  float64 `yaml:"vwap_deviation_pct"`
	// EMAMode switches entry logic: enter on EMA9/EMA21 crossover aligned with 4H bias.
	// TargetRR must be set. Stop goes beyond the crossover candle's wick.
	EMAMode bool `yaml:"ema_mode"`
	// StakeUSDT is the fixed dollar amount risked per trade for PnL reporting. 0 = points only.
	StakeUSDT float64 `yaml:"stake_usd"`
	// BackfillHours is how many hours of 1m klines to fetch from REST on startup. Default 96.
	// Sized so that 4H-signal indicators (EMA21, BB20) prime during backfill. Each engine
	// restart costs (period − backfilled-candles) × signal-tf hours of cold-start blindness;
	// 96h gives 24 closed 4H candles, enough to prime EMA21 + leave 3 candles past the
	// prevEma21 ≠ 0 gate, so signals can fire on the first live close. The single-call
	// Binance limit is 1500 klines (= 25h); larger values trigger pagination — see
	// pkg/marketdata/binance.go backfill().
	BackfillHours int `yaml:"backfill_hours"`
	// SignalTimeframe selects which closed candles drive entry evaluation: "5m" | "30m" | "4H".
	// Defaults to "5m" (legacy). Higher TFs produce wider stops, reducing implicit leverage and fees.
	SignalTimeframe string `yaml:"signal_tf"`
	// ATRStopMult: when > 0 (and EMAMode), stop = entry ± ATR(period) × mult instead of last wick.
	// 0 keeps the legacy wick-based stop.
	ATRStopMult float64 `yaml:"atr_stop_mult"`
	// ATRPeriod: ATR period in candles when ATRStopMult > 0. Defaults to 14.
	ATRPeriod int `yaml:"atr_period"`
}

type BacktestConfig struct {
	CSVPath string `yaml:"csv_path"`
}

type ZoneConfig struct {
	Low  float64 `yaml:"low"`
	High float64 `yaml:"high"`
	Type string  `yaml:"type"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if cfg.Strategy.ProximityPct == 0 {
		cfg.Strategy.ProximityPct = 0.001
	}
	if cfg.Strategy.WickRatio == 0 {
		cfg.Strategy.WickRatio = 2.0
	}
	if cfg.Strategy.BreakoutBodyRatio == 0 {
		cfg.Strategy.BreakoutBodyRatio = 0.6
	}
	if cfg.Strategy.AbsorptionCandles == 0 {
		cfg.Strategy.AbsorptionCandles = 2
	}
	if cfg.Strategy.StopBufferPct == 0 {
		cfg.Strategy.StopBufferPct = 0.001
	}

	if cfg.Exchange.RESTURL == "" {
		cfg.Exchange.RESTURL = "https://fapi.binance.com"
	}
	if cfg.Strategy.BackfillHours == 0 {
		cfg.Strategy.BackfillHours = 96
	}
	if cfg.Strategy.SignalTimeframe == "" {
		cfg.Strategy.SignalTimeframe = "5m"
	}
	if cfg.Strategy.ATRStopMult > 0 && cfg.Strategy.ATRPeriod == 0 {
		cfg.Strategy.ATRPeriod = 14
	}

	if cfg.Symbol == "" {
		return nil, fmt.Errorf("symbol is required in config")
	}

	return &cfg, nil
}

// Zones converts ZoneConfig slice to models.Zone slice.
func (c *Config) ModelZones() []models.Zone {
	out := make([]models.Zone, len(c.Zones))
	for i, z := range c.Zones {
		out[i] = models.Zone{
			Low:  z.Low,
			High: z.High,
			Type: models.ZoneType(z.Type),
		}
	}
	return out
}
