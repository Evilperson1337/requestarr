package logbuffer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newLogger(level slog.Level) (*slog.Logger, *Buffer, *bytes.Buffer) {
	var out bytes.Buffer
	buf := New()
	return slog.New(NewHandler(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: level}), buf)), buf, &out
}

func TestRecordsGoToBothTheWrappedHandlerAndTheBuffer(t *testing.T) {
	logger, buf, out := newLogger(slog.LevelInfo)
	logger.With("component", "sync").WithGroup("job").Warn("sync failed", "source", "romm", "error", errors.New("HTTP 401"))
	logger.Debug("not enabled") // below the level: neither place gets it

	if !strings.Contains(out.String(), "sync failed") {
		t.Fatalf("wrapped handler did not get the record: %q", out.String())
	}
	got := buf.Snapshot(slog.LevelDebug, 0)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	e := got[0]
	if e.Seq != 1 || e.Level != "WARN" || e.Message != "sync failed" {
		t.Fatalf("entry = %+v", e)
	}
	want := []Attr{{"component", "sync"}, {"job.source", "romm"}, {"job.error", "HTTP 401"}}
	if len(e.Attrs) != len(want) {
		t.Fatalf("attrs = %+v", e.Attrs)
	}
	for i := range want {
		if e.Attrs[i] != want[i] {
			t.Fatalf("attr %d = %+v, want %+v", i, e.Attrs[i], want[i])
		}
	}
}

func TestSnapshotFiltersByLevelAndSequence(t *testing.T) {
	logger, buf, _ := newLogger(slog.LevelDebug)
	logger.Info("one")
	logger.Warn("two")
	logger.Error("three")
	if got := buf.Snapshot(slog.LevelWarn, 0); len(got) != 2 || got[0].Message != "two" || got[1].Message != "three" {
		t.Fatalf("warn and above = %+v", got)
	}
	if got := buf.Snapshot(slog.LevelDebug, 2); len(got) != 1 || got[0].Message != "three" {
		t.Fatalf("after 2 = %+v", got)
	}
}

func TestTheRingKeepsTheNewestEntriesOldestFirst(t *testing.T) {
	logger, buf, _ := newLogger(slog.LevelInfo)
	for i := 0; i < Capacity+5; i++ {
		logger.Info("line", "i", i)
	}
	got := buf.Snapshot(slog.LevelDebug, 0)
	if len(got) != Capacity {
		t.Fatalf("kept %d entries, want %d", len(got), Capacity)
	}
	if got[0].Seq != 6 || got[len(got)-1].Seq != Capacity+5 {
		t.Fatalf("first seq %d, last seq %d", got[0].Seq, got[len(got)-1].Seq)
	}
}

func TestLongValuesAreCut(t *testing.T) {
	logger, buf, _ := newLogger(slog.LevelInfo)
	logger.Info(strings.Repeat("m", 5000), "v", strings.Repeat("é", 5000))
	e := buf.Snapshot(slog.LevelDebug, 0)[0]
	if len(e.Message) > maxMessage+len("…") || len(e.Attrs[0].Value) > maxValue+len("…") {
		t.Fatalf("message %d bytes, value %d bytes", len(e.Message), len(e.Attrs[0].Value))
	}
	if !strings.HasSuffix(e.Attrs[0].Value, "é…") {
		t.Fatalf("value was cut inside a character: %q", e.Attrs[0].Value[len(e.Attrs[0].Value)-8:])
	}
}

func TestBufferOf(t *testing.T) {
	logger, buf, _ := newLogger(slog.LevelInfo)
	if BufferOf(logger) != buf {
		t.Fatal("BufferOf did not find the buffer")
	}
	if BufferOf(slog.Default()) != nil || BufferOf(nil) != nil {
		t.Fatal("a plain logger has no buffer")
	}
	// A derived logger still reaches the same buffer through its records.
	logger.With("a", 1).Info("derived")
	if len(buf.Snapshot(slog.LevelDebug, 0)) != 1 {
		t.Fatal("derived logger did not record")
	}
}

func TestAStdoutOnlyRecordIsNotKept(t *testing.T) {
	logger, buf, out := newLogger(slog.LevelInfo)
	logger.InfoContext(StdoutOnly(context.Background()), "first-run password", "one_time_password", "canary-pw")
	if !strings.Contains(out.String(), "canary-pw") {
		t.Fatal("the console must still get the line")
	}
	if got := buf.Snapshot(slog.LevelDebug, 0); len(got) != 0 {
		t.Fatalf("kept %+v", got)
	}
}
