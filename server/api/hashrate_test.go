package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"tarish-server/models"
)

func TestFleetHashrateCountsEachMinerOnce(t *testing.T) {
	_, hs, _ := testAPI(t, "")

	// Two heartbeats per miner, as every minute of 30s reports produces.
	for i, current := range []float64{100, 200} {
		report := fmt.Sprintf(`{"worker_id":"192-168-10-%d","ip":"192.168.10.%d","hashrate":{"current":%g,"average":%g,"max":%g}}`,
			i+1, i+1, current, current, current)
		for j := 0; j < 2; j++ {
			resp, err := http.Post(hs.URL+"/api/report", "application/json", strings.NewReader(report))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("report status = %d", resp.StatusCode)
			}
		}
	}

	resp, err := http.Get(hs.URL + "/api/hashrate/fleet?hours=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var points []models.FleetHashratePoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("fleet hashrate returned no points")
	}

	latest := points[len(points)-1]
	if latest.Hashrate == nil || math.Abs(*latest.Hashrate-300) > 1e-6 {
		t.Fatalf("latest fleet hashrate = %v, want 300", latest.Hashrate)
	}
	if latest.Miners != 2 {
		t.Fatalf("latest miners = %d, want 2", latest.Miners)
	}
}
