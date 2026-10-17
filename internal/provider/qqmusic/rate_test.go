package qqmusic

import (
	"math"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

func TestStandaloneQQRateUsesSharedDescription(t *testing.T) {
	for _, value := range []float64{0, -1, 0.5, 1, 20.5, 30, 45, 60, 90, math.NaN(), math.Inf(1), math.Inf(-1)} {
		p := New(nil, Config{RatePerMin: value}, nil)
		n, per := p.RateLimit()
		want := provider.QQRatePerMinute().Normalize(value)
		if n != 1 || per != time.Duration(float64(time.Minute)/want) {
			t.Fatalf("rate %g: n=%d per=%s want=%g", value, n, per, want)
		}
	}
}
