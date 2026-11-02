package store

import (
	"sort"
	"time"

	"tarish-server/models"
)

// onlineWindow is how long one report vouches for a miner. Agents report
// every 30s, so this rides out a couple of missed reports. minerStatus uses
// it to decide which miners count toward the overview's total hashrate, and
// the fleet series holds each sample for the same span so the two agree.
const onlineWindow = 90 * time.Second

type hashrateSample struct {
	at      time.Time
	current float64
}

// span is a stretch of time measured from the start of a series.
type span struct {
	from, to time.Duration
}

// GetFleetHashrate returns the fleet's total hashrate from since until until
// in step-sized buckets. Buckets are aligned to the clock, so the first one
// starts at or just before since.
//
// Each miner's sample holds until its next report, for at most onlineWindow,
// and a bucket's value is the time-weighted sum of every miner's hashrate
// across it. Summing raw samples instead counts a miner once per report,
// which doubles the total at a 30s report interval and sags in buckets that
// are only partly covered. Stretches in which no miner reported at all (the
// server or its link was down) are left out rather than counted as zero, and
// a bucket with no reports has a nil Hashrate.
func (s *Store) GetFleetHashrate(since, until time.Time, step time.Duration) ([]*models.FleetHashratePoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := since.Truncate(step)
	rows, err := s.db.Query(`
		SELECT miner_id, timestamp, current
		FROM hashrate_history WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, start.Add(-onlineWindow).UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	samples := map[string][]hashrateSample{}
	for rows.Next() {
		var minerID, ts string
		var current float64
		if err := rows.Scan(&minerID, &ts, &current); err != nil {
			return nil, err
		}
		at := parseTime(ts)
		if at.IsZero() {
			continue
		}
		samples[minerID] = append(samples[minerID], hashrateSample{at: at, current: current})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fleetHashrate(samples, start, until, step), nil
}

// fleetHashrate buckets per-miner samples, each in time order, into the
// series described on GetFleetHashrate.
func fleetHashrate(samples map[string][]hashrateSample, start, until time.Time, step time.Duration) []*models.FleetHashratePoint {
	end := until.Sub(start)
	if step <= 0 || end <= 0 {
		return []*models.FleetHashratePoint{}
	}

	n := int((end + step - 1) / step)
	hashes := make([]float64, n) // hashrate × seconds, summed over miners
	observed := make([]time.Duration, n)
	miners := make([]int, n)

	// eachBucket calls fn for every bucket that sp overlaps, with the overlap.
	eachBucket := func(sp span, fn func(b int, overlap time.Duration)) {
		sp.from, sp.to = max(sp.from, 0), min(sp.to, end)
		for b := int(sp.from / step); sp.from < sp.to && b < n && time.Duration(b)*step < sp.to; b++ {
			fn(b, min(sp.to, time.Duration(b+1)*step)-max(sp.from, time.Duration(b)*step))
		}
	}

	// The fleet is observed while reports keep arriving. A silence longer
	// than onlineWindow from every miner at once means the server or its
	// link was down, so that time is unknown rather than zero.
	var arrivals []time.Duration
	for _, series := range samples {
		for _, smp := range series {
			arrivals = append(arrivals, smp.at.Sub(start))
		}
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i] < arrivals[j] })
	var up []span
	for i := 0; i < len(arrivals); {
		sp := span{from: arrivals[i], to: arrivals[i]}
		for i++; i < len(arrivals) && arrivals[i]-sp.to <= onlineWindow; i++ {
			sp.to = arrivals[i]
		}
		up = append(up, sp)
	}
	if last := len(up) - 1; last >= 0 && end-up[last].to <= onlineWindow {
		up[last].to = end // still reporting, so it runs up to now
	}
	for _, sp := range up {
		eachBucket(sp, func(b int, overlap time.Duration) { observed[b] += overlap })
	}

	// Holds stop where their stretch does, and a miner's first report in a
	// stretch also covers back to its start, so miners don't drop out one by
	// one before an outage or trickle back in after it.
	for _, series := range samples {
		k, counted := 0, -1
		for i, smp := range series {
			at := smp.at.Sub(start)
			for up[k].to < at {
				k++
			}
			sp := span{from: at, to: min(at+onlineWindow, up[k].to)}
			if i+1 < len(series) {
				sp.to = min(sp.to, series[i+1].at.Sub(start))
			}
			if (i == 0 || series[i-1].at.Sub(start) < up[k].from) && at-up[k].from <= onlineWindow {
				sp.from = up[k].from
			}
			eachBucket(sp, func(b int, overlap time.Duration) {
				hashes[b] += smp.current * overlap.Seconds()
				if b > counted {
					miners[b]++
					counted = b
				}
			})
		}
	}

	points := make([]*models.FleetHashratePoint, n)
	for b := range points {
		points[b] = &models.FleetHashratePoint{
			Timestamp: start.Add(time.Duration(b) * step),
			Miners:    miners[b],
		}
		if observed[b] > 0 {
			hashrate := hashes[b] / observed[b].Seconds()
			points[b].Hashrate = &hashrate
		}
	}
	return points
}
