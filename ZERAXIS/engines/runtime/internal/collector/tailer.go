package collector

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// Tailer continuously reads newline-delimited JSON from a Falco events file.
type Tailer struct {
	path     string
	events   chan RawFalcoEvent
	dropped  atomic.Int64
	received atomic.Int64
}

// NewTailer creates a tailer with a bounded event channel.
func NewTailer(path string, bufferSize int) *Tailer {
	return &Tailer{
		path:   path,
		events: make(chan RawFalcoEvent, bufferSize),
	}
}

// Events returns the read channel for consuming raw Falco events.
func (t *Tailer) Events() <-chan RawFalcoEvent {
	return t.events
}

// Dropped returns the number of events dropped due to backpressure.
func (t *Tailer) Dropped() int64 {
	return t.dropped.Load()
}

// Received returns the total number of events received.
func (t *Tailer) Received() int64 {
	return t.received.Load()
}

// Run starts tailing the file. Blocks until context is cancelled.
func (t *Tailer) Run(ctx context.Context) {
	defer close(t.events)

	for {
		if err := t.tailFile(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("Falco events file not ready, retrying", "path", t.path, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (t *Tailer) tailFile(ctx context.Context) error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end — only process new events
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	slog.Info("Tailing Falco events file", "path", t.path)

	var partialBuffer []byte
	reader := bufio.NewReader(f)
	
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			partialBuffer = append(partialBuffer, line...)
		}

		if err != nil {
			if err == io.EOF {
				// Check for file truncation (rotation)
				pos, _ := f.Seek(0, io.SeekCurrent)
				info, statErr := f.Stat()
				if statErr != nil || info.Size() < pos {
					slog.Info("Falco events file truncated, reopening")
					return nil // reopen
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}
			return err
		}

		// We have a complete line
		completeLine := partialBuffer
		partialBuffer = nil // reset for next line

		if len(completeLine) <= 1 {
			continue
		}

		var evt RawFalcoEvent
		if err := json.Unmarshal(completeLine, &evt); err != nil {
			slog.Warn("Failed to parse Falco JSON", "error", err)
			continue
		}

		t.received.Add(1)

		select {
		case t.events <- evt:
		default:
			t.dropped.Add(1)
			if t.dropped.Load()%100 == 1 {
				slog.Warn("Event buffer full, dropping event",
					"total_dropped", t.dropped.Load(),
					"rule", evt.Rule,
				)
			}
		}
	}
}
