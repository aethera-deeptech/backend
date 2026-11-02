package gateway

import (
	"encoding/json"
	"fmt"
	"testing"
)

// BenchmarkReadBatch is the cost of reading and checking a batch of 16 nodes with 20 samples each, which is the largest batch that a
// gateway sends
func BenchmarkReadBatch(b *testing.B) {
	var nodes []NodeBatch

	for n := range maxNodes {
		samples := make([]Sample, 0, 20)

		for s := range 20 {
			p, v := 100.5+float64(s), 230.1
			samples = append(samples, Sample{T: 1790000000 + int64(s), P: &p, V: &v})
		}

		nodes = append(nodes, NodeBatch{NodeID: fmt.Sprintf("PLUG-%04d", n), Kind: "plug", Samples: samples})
	}

	body, err := json.Marshal(Batch{Seq: 1, SentAt: 1790000000, Nodes: nodes})
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		var batch Batch
		if err := json.Unmarshal(body, &batch); err != nil {
			b.Fatal(err)
		}

		if err := checkShape(batch); err != nil {
			b.Fatal(err)
		}

		for _, n := range batch.Nodes {
			if !validNode(n) {
				b.Fatal("a node of the batch is refused")
			}

			for _, s := range n.Samples {
				if !validSample(s) {
					b.Fatal("a sample of the batch is refused")
				}
			}
		}
	}
}
