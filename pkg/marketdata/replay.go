package marketdata

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// CSVReplay replays a Binance klines CSV file as a synthetic tick stream.
//
// Binance klines CSV columns (0-indexed):
//
//	0: open_time (Unix ms)
//	1: open
//	2: high
//	3: low
//	4: close
//	5: volume
//	6: close_time (Unix ms)
//
// Each kline is expanded into 4 synthetic ticks (open→high→low→close) with
// timestamps interpolated across the kline window and volume split equally.
// This gives the aggregator enough price/time data to reconstruct 5m/30m/4H candles.
type CSVReplay struct {
	path   string
	symbol string
	file   *os.File
}

func NewCSVReplay(path, symbol string) *CSVReplay {
	return &CSVReplay{path: path, symbol: symbol}
}

func (r *CSVReplay) Subscribe(ctx context.Context) (<-chan models.Tick, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	r.file = f

	ch := make(chan models.Tick, 1000)
	go r.stream(ctx, f, ch)
	return ch, nil
}

func (r *CSVReplay) stream(ctx context.Context, f *os.File, ch chan<- models.Tick) {
	defer close(ch)
	defer f.Close()

	reader := csv.NewReader(f)
	reader.ReuseRecord = true

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		rec, err := reader.Read()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		// Skip header rows (Binance CSVs sometimes have a header).
		if rec[0] == "open_time" || rec[0] == "Open time" {
			continue
		}
		if len(rec) < 7 {
			continue
		}

		openMs, err := strconv.ParseInt(rec[0], 10, 64)
		if err != nil {
			continue
		}
		closeMs, err := strconv.ParseInt(rec[6], 10, 64)
		if err != nil {
			continue
		}

		open, _ := strconv.ParseFloat(rec[1], 64)
		high, _ := strconv.ParseFloat(rec[2], 64)
		low, _ := strconv.ParseFloat(rec[3], 64)
		close_, _ := strconv.ParseFloat(rec[4], 64)
		volume, _ := strconv.ParseFloat(rec[5], 64)

		for _, tick := range expandKlineToTicks(openMs, closeMs, open, high, low, close_, volume, r.symbol) {
			select {
			case <-ctx.Done():
				return
			case ch <- tick:
			}
		}
	}
}

func (r *CSVReplay) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
