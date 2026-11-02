package store

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"tarish-server/models"
)

// fleetT0 is a bucket boundary; test times are offsets from it.
var fleetT0 = time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)

// heartbeats returns one miner's samples at a steady hashrate between two
// offsets from fleetT0, about every 30s but anywhere from 26s to 34s apart,
// so a given minute can hold one, two or three reports.
func heartbeats(rng *rand.Rand, from, to time.Duration, current float64) []hashrateSample {
	var out []hashrateSample
	for d := from + time.Duration(rng.Intn(30))*time.Second; d <= to; d += time.Duration(26+rng.Intn(9)) * time.Second {
		out = append(out, hashrateSample{at: fleetT0.Add(d), current: current})
	}
	return out
}

func assertHashrate(t *testing.T, p *models.FleetHashratePoint, want float64) {
	t.Helper()
	if p.Hashrate == nil {
		t.Fatalf("bucket %s hashrate = nil, want %.2f", p.Timestamp.Format("15:04"), want)
	}
	if math.Abs(*p.Hashrate-want) > 1e-6 {
		t.Fatalf("bucket %s hashrate = %.2f, want %.2f", p.Timestamp.Format("15:04"), *p.Hashrate, want)
	}
}

func TestFleetHashrateCountsEachMinerOncePerBucket(t *testing.T) {
	step := 5 * time.Minute
	since := fleetT0.Add(2*time.Minute + 34*time.Second) // not on a bucket boundary

	for _, until := range []time.Duration{
		60*time.Minute + 2*time.Second, // last bucket only just started
		62*time.Minute + 34*time.Second,
		64*time.Minute + 59*time.Second,
	} {
		rng := rand.New(rand.NewSource(1))
		samples := map[string][]hashrateSample{
			"a": heartbeats(rng, -10*time.Minute, until, 100),
			"b": heartbeats(rng, -10*time.Minute, until, 200),
		}

		points := fleetHashrate(samples, since.Truncate(step), fleetT0.Add(until), step)

		if len(points) != 13 {
			t.Fatalf("until +%s: len = %d, want 13", until, len(points))
		}
		if !points[0].Timestamp.Equal(fleetT0) {
			t.Fatalf("until +%s: first bucket = %s, want %s", until, points[0].Timestamp, fleetT0)
		}
		for _, p := range points {
			assertHashrate(t, p, 300)
			if p.Miners != 2 {
				t.Fatalf("until +%s: bucket %s miners = %d, want 2", until, p.Timestamp.Format("15:04"), p.Miners)
			}
		}
	}
}

func TestFleetHashrateLeavesOutTimeWithNoReports(t *testing.T) {
	// Nothing reports between about +22m and +38m, as when the server is down.
	rng := rand.New(rand.NewSource(2))
	samples := map[string][]hashrateSample{
		"a": append(heartbeats(rng, -time.Minute, 22*time.Minute, 100), heartbeats(rng, 38*time.Minute, time.Hour, 100)...),
		"b": append(heartbeats(rng, -time.Minute, 22*time.Minute, 200), heartbeats(rng, 38*time.Minute, time.Hour, 200)...),
	}

	points := fleetHashrate(samples, fleetT0, fleetT0.Add(time.Hour), 5*time.Minute)

	for i, p := range points {
		if i == 5 || i == 6 {
			if p.Hashrate != nil || p.Miners != 0 {
				t.Fatalf("bucket %s has data from %d miners, want none", p.Timestamp.Format("15:04"), p.Miners)
			}
			continue
		}
		// Includes 08:20 and 08:35, which the outage only partly covers.
		assertHashrate(t, p, 300)
	}
}

func TestFleetHashrateBridgesMissedReports(t *testing.T) {
	// A quick server restart at +20m loses whatever arrives in a 25s window,
	// so each miner misses at most one report.
	restart := func(series []hashrateSample) []hashrateSample {
		var kept []hashrateSample
		for _, smp := range series {
			if d := smp.at.Sub(fleetT0); d < 20*time.Minute || d >= 20*time.Minute+25*time.Second {
				kept = append(kept, smp)
			}
		}
		return kept
	}
	rng := rand.New(rand.NewSource(5))
	samples := map[string][]hashrateSample{
		"a": restart(heartbeats(rng, -time.Minute, time.Hour, 100)),
		"b": restart(heartbeats(rng, -time.Minute, time.Hour, 200)),
	}

	for _, p := range fleetHashrate(samples, fleetT0, fleetT0.Add(time.Hour), 5*time.Minute) {
		assertHashrate(t, p, 300)
	}
}

func TestFleetHashrateDropsMinerThatStopsReporting(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	samples := map[string][]hashrateSample{
		"a": heartbeats(rng, -time.Minute, time.Hour, 100),
		"b": heartbeats(rng, -time.Minute, 20*time.Minute, 200),
	}

	points := fleetHashrate(samples, fleetT0, fleetT0.Add(time.Hour), 5*time.Minute)

	assertHashrate(t, points[3], 300)
	// b still counts until onlineWindow after its last report, then not at all.
	if h := points[4].Hashrate; h == nil || *h <= 100 || *h >= 300 {
		t.Fatalf("bucket 08:20 hashrate = %v, want between 100 and 300", h)
	}
	for _, p := range points[5:] {
		assertHashrate(t, p, 100)
		if p.Miners != 1 {
			t.Fatalf("bucket %s miners = %d, want 1", p.Timestamp.Format("15:04"), p.Miners)
		}
	}
}

func TestGetFleetHashrateReadsHistory(t *testing.T) {
	s := newTestStore(t)

	rng := rand.New(rand.NewSource(4))
	for _, miner := range []struct {
		id      string
		current float64
	}{{"192-168-10-36", 65710}, {"192-168-11-80", 71910}} {
		for _, smp := range heartbeats(rng, -10*time.Minute, 32*time.Minute, miner.current) {
			if _, err := s.db.Exec(`
				INSERT INTO hashrate_history (miner_id, timestamp, current, average, max)
				VALUES (?, ?, ?, ?, ?)
			`, miner.id, smp.at.Format(time.RFC3339), smp.current, smp.current, smp.current); err != nil {
				t.Fatalf("seed history: %v", err)
			}
		}
	}

	points, err := s.GetFleetHashrate(fleetT0.Add(2*time.Minute+34*time.Second), fleetT0.Add(32*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatalf("GetFleetHashrate: %v", err)
	}

	if len(points) != 7 {
		t.Fatalf("len = %d, want 7", len(points))
	}
	if !points[0].Timestamp.Equal(fleetT0) {
		t.Fatalf("first bucket = %s, want %s", points[0].Timestamp, fleetT0)
	}
	for _, p := range points {
		assertHashrate(t, p, 65710+71910)
	}
}
