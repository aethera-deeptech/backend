package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewWritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer

	New(&buf).Info("started", "port", 8080)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("the line is not JSON %v: %s", err, buf.String())
	}

	if line["msg"] != "started" || line["level"] != "INFO" {
		t.Fatalf("unexpected line %v", line)
	}

	if line["port"] != float64(8080) {
		t.Fatalf("the attribute port is %v", line["port"])
	}
}
