// Package logging sends every log line of the process to standard output as one JSON object, so a log shipper can read the
// fields without a pattern for each kind of line.
package logging

import (
	"io"
	"log/slog"
	"os"
)

// Setup makes slog write JSON to standard output and sends what the log package prints through the same handler. A line
// from log.Printf becomes an object with its text in msg, so the lines of the packages that use log need no change.
func Setup() {
	slog.SetDefault(New(os.Stdout))
}

// New returns a logger that writes one JSON object per line to w
func New(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
