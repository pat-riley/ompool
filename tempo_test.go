package main

import (
	"math"
	"testing"
)

func TestBusynessSpansTheTempoRange(t *testing.T) {
	if got := busyness(0, 0); got != 0 {
		t.Fatalf("an empty network should be 0 busy, got %v", got)
	}
	if got := busyness(1e6, 1e7); math.Abs(got-1) > 1e-9 {
		t.Fatalf("a saturated network should be 1 busy, got %v", got)
	}
	calm, busy := busyness(500, 8000), busyness(3000, 120000)
	if !(calm < busy) {
		t.Fatalf("more inflow and backlog should read busier: calm=%v busy=%v", calm, busy)
	}
	if lo, hi := tempoForBusyness(0), tempoForBusyness(1); lo != minTempoBPM || hi != maxTempoBPM {
		t.Fatalf("tempo range = %v..%v, want %v..%v", lo, hi, minTempoBPM, maxTempoBPM)
	}
}

func TestTempoEasesTowardTarget(t *testing.T) {
	m := newModel("")
	m.inflow, m.overview.Mempool.Count = 500, 8000
	m.updateTempo()
	start := m.audioBPM
	if start == 0 {
		t.Fatal("first sample should snap to a tempo")
	}
	m.inflow, m.overview.Mempool.Count = 5000, 180000
	m.updateTempo()
	target := tempoForBusyness(busyness(5000, 180000))
	if !(m.audioBPM > start && m.audioBPM < target) {
		t.Fatalf("tempo should ease toward %v from %v, got %v", target, start, m.audioBPM)
	}
}
