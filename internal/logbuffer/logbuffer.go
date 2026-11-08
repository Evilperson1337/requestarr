// Package logbuffer keeps the most recent server log records in memory so a
// superuser can read them in Settings without shell or Docker access.
//
// Handler wraps the process's real slog handler: every record still goes to
// it unchanged, and a copy goes into a fixed-size ring. Nothing is written to
// disk or the database, and a restart clears the history. It records exactly
// what the server already logs, so the rules that keep secrets and personal
// data out of logs apply to it unchanged.
package logbuffer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Capacity is how many records the ring keeps.
const Capacity = 1000

const (
	maxMessage = 1000
	maxValue   = 2000
	maxAttrs   = 32
)

// Attr is one key and its value, as text.
type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Entry is one log record.
type Entry struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Attrs   []Attr    `json:"attrs"`
}

// Buffer is a ring of the most recent entries. A nil Buffer records nothing.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	full    bool
	seq     uint64
}

// New returns an empty buffer holding up to Capacity entries.
func New() *Buffer { return &Buffer{entries: make([]Entry, Capacity)} }

func (b *Buffer) add(e Entry) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e.Seq = b.seq
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

// Snapshot returns the entries at or above minLevel with a sequence number
// greater than after, oldest first.
func (b *Buffer) Snapshot(minLevel slog.Level, after uint64) []Entry {
	out := []Entry{}
	if b == nil {
		return out
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	start, n := 0, b.next
	if b.full {
		start, n = b.next, len(b.entries)
	}
	for i := 0; i < n; i++ {
		e := b.entries[(start+i)%len(b.entries)]
		if e.Seq > after && levelOf(e.Level) >= minLevel {
			out = append(out, e)
		}
	}
	return out
}

func levelOf(name string) slog.Level {
	var l slog.Level
	if l.UnmarshalText([]byte(name)) != nil {
		return slog.LevelInfo
	}
	return l
}

type stdoutOnlyKey struct{}

// StdoutOnly marks a context so a record logged with it goes to the wrapped
// handler (standard output) and is not kept for Settings, Logs. It is for the
// rare line that must carry a secret to the operator's console, such as the
// local administrator's first-run password (D040).
func StdoutOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, stdoutOnlyKey{}, true)
}

// Handler sends each record to the wrapped handler and keeps a copy.
type Handler struct {
	inner  slog.Handler
	buf    *Buffer
	attrs  []Attr // from WithAttrs, already prefixed with their groups
	prefix string // open groups, "a.b."
}

// NewHandler wraps inner so that what it handles is also kept in buf.
func NewHandler(inner slog.Handler, buf *Buffer) *Handler {
	return &Handler{inner: inner, buf: buf}
}

// Buffer returns the buffer a logger's records are kept in, or nil when the
// logger was not built with a Handler.
func BufferOf(logger *slog.Logger) *Buffer {
	if logger == nil {
		return nil
	}
	if h, ok := logger.Handler().(*Handler); ok {
		return h.buf
	}
	return nil
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil && ctx.Value(stdoutOnlyKey{}) != nil {
		return h.inner.Handle(ctx, r)
	}
	e := Entry{Time: r.Time, Level: r.Level.String(), Message: truncate(r.Message, maxMessage), Attrs: make([]Attr, 0, len(h.attrs)+r.NumAttrs())}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Attrs = append(e.Attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		e.Attrs = appendAttr(e.Attrs, h.prefix, a)
		return len(e.Attrs) < maxAttrs
	})
	if len(e.Attrs) > maxAttrs {
		e.Attrs = e.Attrs[:maxAttrs]
	}
	h.buf.add(e)
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.inner = h.inner.WithAttrs(attrs)
	next.attrs = append([]Attr(nil), h.attrs...)
	for _, a := range attrs {
		next.attrs = appendAttr(next.attrs, h.prefix, a)
	}
	return &next
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.inner = h.inner.WithGroup(name)
	next.prefix = h.prefix + name + "."
	return &next
}

// appendAttr flattens a (possibly grouped) attribute into key=value pairs.
func appendAttr(out []Attr, prefix string, a slog.Attr) []Attr {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range v.Group() {
			out = appendAttr(out, p, g)
		}
		return out
	}
	if a.Key == "" && v.Any() == nil {
		return out
	}
	var text string
	switch v.Kind() {
	case slog.KindTime:
		text = v.Time().UTC().Format(time.RFC3339)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			text = err.Error()
		} else {
			text = fmt.Sprint(v.Any())
		}
	default:
		text = v.String()
	}
	return append(out, Attr{Key: truncate(prefix+a.Key, 200), Value: truncate(text, maxValue)})
}

// truncate shortens s to at most limit bytes without splitting a character.
func truncate(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
