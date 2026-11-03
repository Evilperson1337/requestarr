// Package devlogs records only allowlisted connection-check diagnostics in memory.
// It must not receive arbitrary log output, errors, URLs, or response payloads.
package devlogs

import (
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/setup"
)

const Capacity = 200

type Stage string

const (
	StageCheck     Stage = "check"
	StageRequest   Stage = "request"
	StageOAuth     Stage = "oauth"
	StagePlatforms Stage = "platforms"
)

type Code string

const (
	CodeConnected      Code = "connected"
	CodeFailed         Code = "connection_failed"
	CodeAuthentication Code = "authentication_failed"
	CodeEnvironment    Code = "environment_required"
	CodeIncomplete     Code = "incomplete_settings"
	CodeDisabled       Code = "disabled"
	CodeHTTPStatus     Code = "http_status"
	CodeNetwork        Code = "network"
	CodeTimeout        Code = "network_timeout"
	CodeDNS            Code = "network_dns"
	CodeContentType    Code = "content_type"
	CodeResponseSize   Code = "response_size"
	CodeMalformed      Code = "malformed_response"
)

type Entry struct {
	Seq        uint64       `json:"seq"`
	Time       time.Time    `json:"time"`
	Target     setup.Target `json:"target"`
	Stage      Stage        `json:"stage"`
	Outcome    string       `json:"outcome"`
	Code       Code         `json:"code"`
	HTTPStatus int          `json:"httpStatus,omitempty"`
	DurationMs int64        `json:"durationMs"`
}

type Snapshot struct {
	Entries  []Entry `json:"entries"`
	Capacity int     `json:"capacity"`
}

// Recorder is a fixed-size ring. Its zero value is ready for use; a nil pointer
// disables recording. Snapshot copies entries in oldest-to-newest order.
type Recorder struct {
	mu      sync.Mutex
	entries [Capacity]Entry
	seq     uint64
	count   int
}

// Record accepts only fixed enums, never untrusted strings. Unknown enum values
// are dropped at this boundary as defense against accidental secret retention.
func (r *Recorder) Record(target setup.Target, stage Stage, code Code, status int, duration time.Duration) {
	if r == nil || !target.Valid() {
		return
	}
	switch stage {
	case StageCheck, StageRequest, StageOAuth, StagePlatforms:
	default:
		return
	}
	outcome := "failure"
	switch code {
	case CodeConnected:
		outcome = "success"
	case CodeDisabled:
		outcome = "skipped"
	case CodeFailed, CodeAuthentication, CodeEnvironment, CodeIncomplete, CodeHTTPStatus, CodeNetwork, CodeTimeout, CodeDNS, CodeContentType, CodeResponseSize, CodeMalformed:
	default:
		return
	}
	if status < 100 || status > 599 {
		status = 0
	}
	if duration < 0 {
		duration = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[r.seq%Capacity] = Entry{Seq: r.seq + 1, Time: time.Now().UTC(), Target: target, Stage: stage, Outcome: outcome, Code: code, HTTPStatus: status, DurationMs: duration.Milliseconds()}
	r.seq++
	if r.count < Capacity {
		r.count++
	}
}

func (r *Recorder) Snapshot() Snapshot {
	out := Snapshot{Entries: []Entry{}, Capacity: Capacity}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for seq := r.seq - uint64(r.count); seq < r.seq; seq++ {
		out.Entries = append(out.Entries, r.entries[seq%Capacity])
	}
	return out
}
