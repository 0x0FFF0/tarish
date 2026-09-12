package snellspec

// Built-in seed used as an out-of-pool fallback. Display as 内置兜底.
const SeedID = "c3a91f0e7b2d4a68b1e05c9d7f3a2468"

func SeedNode() NodeSpec {
	return NodeSpec{
		ID:      SeedID,
		Host:    "104.194.65.101",
		Port:    60872,
		PSK:     "TlSAvIt8/rrRCe5HkKrRWA==",
		Version: "v5",
		Builtin: true,
	}
}

func IsSeedTransport(n NodeSpec) bool {
	return n.TransportEqual(SeedNode())
}

func IsSeed(n NodeSpec) bool {
	return n.ID == SeedID || n.Builtin || IsSeedTransport(n)
}
