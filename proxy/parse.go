package proxy

import "tarish/snellspec"

func ParseDocument(raw []byte, format Format) (*Snapshot, error) {
	return snellspec.ParseDocument(raw, format)
}

func ParseFormat(s string) (Format, error) {
	return snellspec.ParseFormat(s)
}
