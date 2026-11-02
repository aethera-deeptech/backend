package gateway

import (
	"encoding/json"
	"testing"
)

// FuzzBatch reads any bytes as the body of a batch and runs every check of the intake on what it finds. None of the checks may
// panic, whatever a gateway sends.
func FuzzBatch(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"seq":1,"sentAt":1790000000,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":1790000000,"p":12.5}]}]}`,
		`{"seq":1,"sentAt":1790000000,"nodes":[{"nodeId":"CLAMP-0001","kind":"clamp","hops":2,"samples":[{"t":1,"p":1e308,"v":-1,"a":9}]}]}`,
		`{"seq":-1,"sentAt":0,"nodes":null}`,
		`{"nodes":[{"samples":[{}]}]}`,
		`[]`,
		`not json`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(_ *testing.T, body []byte) {
		var b Batch
		if json.Unmarshal(body, &b) != nil {
			return
		}

		_ = checkShape(b)

		for _, n := range b.Nodes {
			_ = validNode(n)

			for _, s := range n.Samples {
				_ = validSample(s)
			}
		}
	})
}
