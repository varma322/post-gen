package db

import (
	"math"
	"testing"
)

func TestSuccessRate(t *testing.T) {
	cases := []struct {
		success, failed int
		want            float64
	}{
		{8, 2, 80},
		{0, 0, 0}, // nothing attempted
		{7, 0, 100},
		{0, 5, 0},
		{1, 2, 33.3},
		{2, 1, 66.7},
	}

	for _, tc := range cases {
		if got := successRate(tc.success, tc.failed); got != tc.want {
			t.Errorf("successRate(%d, %d) = %v, want %v", tc.success, tc.failed, got, tc.want)
		}
	}
}

func TestNewDeltaRoundsNegativeChangesCorrectly(t *testing.T) {
	// Regression: the truncating int(x*10+0.5) idiom rounds toward zero, so a
	// clean -25% arrived as -24.9 on the dashboard.
	delta := NewDelta(6, 8)

	if delta.PctChange == nil {
		t.Fatal("expected a percentage change")
	}
	if *delta.PctChange != -25 {
		t.Errorf("PctChange = %v, want -25", *delta.PctChange)
	}
}

func TestNewDeltaHandlesGrowth(t *testing.T) {
	delta := NewDelta(12, 10)
	if delta.PctChange == nil || *delta.PctChange != 20 {
		t.Errorf("PctChange = %v, want 20", delta.PctChange)
	}
	if delta.Current != 12 || delta.Previous != 10 {
		t.Errorf("delta lost its raw counts: %+v", delta)
	}
}

func TestNewDeltaOmitsPercentageFromZeroBase(t *testing.T) {
	// Growth from nothing has no meaningful percentage; the UI should render a
	// dash rather than "∞%" or a misleading 100%.
	delta := NewDelta(5, 0)
	if delta.PctChange != nil {
		t.Errorf("expected no percentage from a zero base, got %v", *delta.PctChange)
	}
	if delta.Current != 5 {
		t.Errorf("Current = %d, want 5", delta.Current)
	}
}

func TestRound1(t *testing.T) {
	cases := map[float64]float64{
		-25.0:  -25,
		-24.96: -25,
		24.96:  25,
		33.333: 33.3,
		66.666: 66.7,
		0:      0,
	}

	for input, want := range cases {
		if got := round1(input); math.Abs(got-want) > 1e-9 {
			t.Errorf("round1(%v) = %v, want %v", input, got, want)
		}
	}
}
