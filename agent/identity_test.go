package agent

import "testing"

func TestPreferredAgentID(t *testing.T) {
	tests := []struct {
		name     string
		minerID  string
		workerID string
		ip       string
		hostname string
		want     string
	}{
		{
			name:     "prefers worker id",
			minerID:  "epyc-0",
			workerID: "192-168-10-36",
			ip:       "192.168.10.36",
			hostname: "mz73-0074",
			want:     "192-168-10-36",
		},
		{
			name:     "falls back to ip",
			ip:       "192.168.10.210",
			hostname: "CT100",
			want:     "192-168-10-210",
		},
		{
			name:     "falls back to hostname",
			hostname: "CT100",
			want:     "CT100",
		},
		{
			name:    "falls back to miner id",
			minerID: "epyc-0",
			want:    "epyc-0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preferredAgentID(tt.minerID, tt.workerID, tt.ip, tt.hostname)
			if got != tt.want {
				t.Fatalf("preferredAgentID(%q, %q, %q, %q) = %q, want %q",
					tt.minerID, tt.workerID, tt.ip, tt.hostname, got, tt.want)
			}
		})
	}
}
