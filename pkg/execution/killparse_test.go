package execution

import (
	"strings"
	"testing"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestParseClosePositionSpec_HappyPath_Single(t *testing.T) {
	got, err := ParseClosePositionSpec("BTCUSDT,LONG,0.5")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Symbol != "BTCUSDT" || got[0].Side != models.Long || got[0].Quantity != 0.5 {
		t.Errorf("got %+v", got[0])
	}
}

func TestParseClosePositionSpec_HappyPath_Multiple(t *testing.T) {
	got, err := ParseClosePositionSpec("BTCUSDT,LONG,0.5;ETHUSDT,SHORT,2.0;XLMUSDT,SHORT,1000")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Side != models.Long || got[1].Side != models.Short || got[2].Side != models.Short {
		t.Errorf("sides wrong: %+v / %+v / %+v", got[0], got[1], got[2])
	}
	if got[2].Quantity != 1000 {
		t.Errorf("qty: got %v want 1000", got[2].Quantity)
	}
}

func TestParseClosePositionSpec_CaseInsensitiveSide_UppercasesSymbol(t *testing.T) {
	got, err := ParseClosePositionSpec("btcusdt,long,0.5")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got[0].Symbol != "BTCUSDT" {
		t.Errorf("symbol not uppercased: %q", got[0].Symbol)
	}
	if got[0].Side != models.Long {
		t.Errorf("lowercase 'long' not parsed: %v", got[0].Side)
	}
}

func TestParseClosePositionSpec_WhitespaceTolerant(t *testing.T) {
	got, err := ParseClosePositionSpec("  BTCUSDT , LONG , 0.5 ;  ETHUSDT,SHORT,2  ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0].Symbol != "BTCUSDT" || got[1].Symbol != "ETHUSDT" {
		t.Errorf("symbols: %q / %q", got[0].Symbol, got[1].Symbol)
	}
}

func TestParseClosePositionSpec_TrailingSemicolonTolerated(t *testing.T) {
	got, err := ParseClosePositionSpec("BTCUSDT,LONG,0.5;")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestParseClosePositionSpec_EmptySpec_Errors(t *testing.T) {
	_, err := ParseClosePositionSpec("")
	if err == nil {
		t.Error("expected err on empty spec")
	}
	_, err = ParseClosePositionSpec("   ")
	if err == nil {
		t.Error("expected err on whitespace-only spec")
	}
}

func TestParseClosePositionSpec_OnlyDelimiters_Errors(t *testing.T) {
	_, err := ParseClosePositionSpec(";;;")
	if err == nil {
		t.Error("expected err on delimiter-only spec")
	}
}

func TestParseClosePositionSpec_TooFewFields(t *testing.T) {
	cases := []string{
		"BTCUSDT,LONG",
		"BTCUSDT",
		"BTCUSDT,LONG,0.5,extra",
	}
	for _, spec := range cases {
		if _, err := ParseClosePositionSpec(spec); err == nil {
			t.Errorf("spec %q: expected err", spec)
		}
	}
}

func TestParseClosePositionSpec_InvalidSide(t *testing.T) {
	cases := []string{
		"BTCUSDT,sideways,0.5",
		"BTCUSDT,NEUTRAL,0.5", // explicitly rejected — close orders must be directional
		"BTCUSDT,,0.5",
	}
	for _, spec := range cases {
		_, err := ParseClosePositionSpec(spec)
		if err == nil {
			t.Errorf("spec %q: expected err", spec)
			continue
		}
		if !strings.Contains(err.Error(), "side") {
			t.Errorf("spec %q: err should mention side: %v", spec, err)
		}
	}
}

func TestParseClosePositionSpec_NonPositiveQty(t *testing.T) {
	cases := []string{
		"BTCUSDT,LONG,0",
		"BTCUSDT,LONG,-1",
		"BTCUSDT,LONG,-0.5",
	}
	for _, spec := range cases {
		_, err := ParseClosePositionSpec(spec)
		if err == nil {
			t.Errorf("spec %q: expected err", spec)
		}
	}
}

func TestParseClosePositionSpec_NonNumericQty(t *testing.T) {
	_, err := ParseClosePositionSpec("BTCUSDT,LONG,abc")
	if err == nil {
		t.Error("expected err on non-numeric qty")
	}
}

func TestParseClosePositionSpec_EmptySymbol(t *testing.T) {
	_, err := ParseClosePositionSpec(",LONG,0.5")
	if err == nil {
		t.Error("expected err on empty symbol")
	}
}

func TestParseClosePositionSpec_FailFast_FirstError(t *testing.T) {
	// Malformed first tuple should error before second valid tuple is examined.
	_, err := ParseClosePositionSpec("BTCUSDT,sideways,0.5;ETHUSDT,SHORT,2")
	if err == nil {
		t.Fatal("expected err on first invalid tuple")
	}
	if !strings.Contains(err.Error(), "sideways") {
		t.Errorf("err should reference the malformed value: %v", err)
	}
}
