package gateway

import (
	"math"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func at(h, m, s int) time.Time { return time.Date(2026, 11, 2, h, m, s, 0, time.UTC) }

func near(t *testing.T, name string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s is %v, want %v", name, got, want)
	}
}

func TestEnergyUsesTheCounterWhenItGrewByAPlausibleAmount(t *testing.T) {
	prevAt := at(10, 0, 0)

	near(t, "counter", energyBetween(prevAt, f(5), stored{t: at(10, 1, 0), watts: 600, energy: f(5.01)}), 0.01)
	// no counter on one side, so the power times the time is used
	near(t, "no counter", energyBetween(prevAt, nil, stored{t: at(10, 1, 0), watts: 600}), 0.01)
}

func TestEnergyFallsBackWhenTheCounterRestartsOrJumps(t *testing.T) {
	prevAt := at(10, 0, 0)

	// the node restarted and its counter began again from zero
	near(t, "restart", energyBetween(prevAt, f(120), stored{t: at(10, 1, 0), watts: 600, energy: f(0.002)}), 0.01)
	// the counter jumped by 1000 kWh in a minute, which no node can measure
	near(t, "jump", energyBetween(prevAt, f(5), stored{t: at(10, 1, 0), watts: 600, energy: f(1005)}), 0.01)
}

func TestAGapCountsOnlyUpToTheLimitWhenThereIsNoCounter(t *testing.T) {
	// one hour without a sample must not become one hour of the last power
	near(t, "gap", energyBetween(at(10, 0, 0), nil, stored{t: at(11, 0, 0), watts: 1000}), 1000.0/1000*5.0/60)
	near(t, "no time", energyBetween(at(10, 0, 0), nil, stored{t: at(10, 0, 0), watts: 1000}), 0)
}

func TestAggregateKeepsTheSpikeAndSplitsTheHours(t *testing.T) {
	prev := state{at: ptr(at(9, 59, 0)), energy: f(10)}
	samples := []stored{
		{t: at(10, 30, 0), watts: 310, energy: f(10.1)},
		{t: at(10, 10, 0), watts: 305, energy: f(10.05)},
		{t: at(10, 20, 0), watts: 2000, energy: f(10.08)},
		{t: at(11, 5, 0), watts: 300, energy: f(10.2)},
	}

	got, next := aggregate(prev, samples)

	h10, h11 := got[at(10, 0, 0)], got[at(11, 0, 0)]
	if h10 == nil || h11 == nil || len(got) != 2 {
		t.Fatalf("the hours are %v", got)
	}

	if h10.samples != 3 || h10.maxWatts != 2000 || h10.minWatts != 305 || h10.sumWatts != 2615 {
		t.Errorf("hour 10 is %+v", h10)
	}

	near(t, "energy of hour 10", h10.energy, 0.1)
	near(t, "energy of hour 11", h11.energy, 0.1)

	if next.at == nil || !next.at.Equal(at(11, 5, 0)) || next.energy == nil || *next.energy != 10.2 {
		t.Errorf("the state moved to %+v", next)
	}
}

func TestALateSampleAddsPowerFiguresButNoEnergy(t *testing.T) {
	prev := state{at: ptr(at(10, 30, 0)), energy: f(10.5)}

	got, next := aggregate(prev, []stored{{t: at(10, 10, 0), watts: 900, energy: f(10.2)}})
	b := got[at(10, 0, 0)]

	if b.samples != 1 || b.maxWatts != 900 {
		t.Errorf("the late sample was not counted in the power figures %+v", b)
	}

	near(t, "energy", b.energy, 0)

	if !next.at.Equal(at(10, 30, 0)) {
		t.Errorf("the state moved back to %v", next.at)
	}
}

func TestTheFirstSampleOfANodeHasNoEnergy(t *testing.T) {
	got, next := aggregate(state{}, []stored{{t: at(10, 0, 0), watts: 500, energy: f(1)}})

	near(t, "energy", got[at(10, 0, 0)].energy, 0)

	if next.at == nil || *next.energy != 1 {
		t.Errorf("the state is %+v", next)
	}
}

func ptr(t time.Time) *time.Time { return &t }
