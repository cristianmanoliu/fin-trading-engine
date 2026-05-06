package strategy

import (
	"strings"
	"testing"
)

func TestParseShadowSpecs_Empty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		specs, err := ParseShadowSpecs(in)
		if err != nil {
			t.Errorf("empty input %q should return nil error, got %v", in, err)
		}
		if specs != nil {
			t.Errorf("empty input %q should return nil specs, got %v", in, specs)
		}
	}
}

func TestParseShadowSpecs_Single(t *testing.T) {
	specs, err := ParseShadowSpecs("alt1:5-15-336")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 spec, got %d", len(specs))
	}
	got := specs[0]
	if got.Label != "alt1" || got.EMAFastPeriod != 5 || got.EMASlowPeriod != 15 || got.MaxHoldHours != 336 {
		t.Errorf("got %+v, want {alt1, 5, 15, 336}", got)
	}
}

func TestParseShadowSpecs_Multiple(t *testing.T) {
	specs, err := ParseShadowSpecs("a:5-15-336,b:9-21-504,c:12-26-168")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("want 3 specs, got %d", len(specs))
	}
	if specs[0].Label != "a" || specs[0].EMAFastPeriod != 5 {
		t.Errorf("specs[0] wrong: %+v", specs[0])
	}
	if specs[1].Label != "b" || specs[1].MaxHoldHours != 504 {
		t.Errorf("specs[1] wrong: %+v", specs[1])
	}
	if specs[2].Label != "c" || specs[2].EMAFastPeriod != 12 || specs[2].EMASlowPeriod != 26 {
		t.Errorf("specs[2] wrong: %+v", specs[2])
	}
}

func TestParseShadowSpecs_MaxHoldZero(t *testing.T) {
	// max_hold=0 means "no cap" — must be allowed
	specs, err := ParseShadowSpecs("nocap:5-15-0")
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].MaxHoldHours != 0 {
		t.Errorf("max_hold=0 not preserved: %v", specs[0].MaxHoldHours)
	}
}

func TestParseShadowSpecs_MalformedRejected(t *testing.T) {
	cases := []string{
		"missing-colon",                   // no label separator
		"label:5-15",                      // only 2 params, need 3
		"label:5-15-336-extra",            // 4 params
		"label:abc-15-336",                // non-numeric ema_fast
		"label:5-xyz-336",                 // non-numeric ema_slow
		"label:5-15-abc",                  // non-numeric max_hold
		"label:0-15-336",                  // zero ema_fast (must be positive)
		"label:5-0-336",                   // zero ema_slow
		"label:-5-15-336",                 // negative ema_fast
		"label:5-15--336",                 // negative max_hold
	}
	for _, c := range cases {
		_, err := ParseShadowSpecs(c)
		if err == nil {
			t.Errorf("expected parse error for %q, got nil", c)
		} else if !strings.Contains(err.Error(), "shadow spec") {
			t.Errorf("error message should mention 'shadow spec' for %q: %v", c, err)
		}
	}
}

func TestParseShadowSpecs_SkipsEmptyEntries(t *testing.T) {
	// Trailing comma / double comma should be tolerated
	specs, err := ParseShadowSpecs("a:5-15-336,,b:9-21-504,")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Errorf("want 2 specs ignoring empties, got %d: %+v", len(specs), specs)
	}
}
