package models

// ZoneType classifies a supply or demand zone.
type ZoneType string

const (
	Supply ZoneType = "supply"
	Demand ZoneType = "demand"
)

// Zone represents a price level range where supply or demand is expected.
type Zone struct {
	Low  float64
	High float64
	Type ZoneType
}

// Contains reports whether price falls inside this zone.
func (z Zone) Contains(price float64) bool {
	return price >= z.Low && price <= z.High
}

// NearEdge reports whether price is within pct (e.g. 0.001 = 0.1%) of either zone edge.
func (z Zone) NearEdge(price, pct float64) bool {
	threshold := price * pct
	return abs(price-z.Low) <= threshold || abs(price-z.High) <= threshold
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
