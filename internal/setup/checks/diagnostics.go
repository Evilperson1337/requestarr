package checks

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/setup"
)

type diagnosticTargetKey struct{}

// probeDiagnostic is local to one probe, never shared between checks. It holds
// only enums and numbers; error values and request/response data never enter it.
type probeDiagnostic struct {
	recorder *devlogs.Recorder
	logger   *slog.Logger
	target   setup.Target
	stage    devlogs.Stage
	code     devlogs.Code
	status   int
	started  time.Time
}

func (c *Checker) probe(ctx context.Context, stage devlogs.Stage) *probeDiagnostic {
	target, _ := ctx.Value(diagnosticTargetKey{}).(setup.Target)
	return &probeDiagnostic{recorder: c.config.Diagnostics, logger: c.config.Logger, target: target, stage: stage, code: devlogs.CodeMalformed, started: time.Now()}
}

func (p *probeDiagnostic) finish() {
	elapsed := time.Since(p.started)
	p.recorder.Record(p.target, p.stage, p.code, p.status, elapsed)
	if p.logger != nil && p.code != devlogs.CodeConnected {
		p.logger.Warn("connection check failed", "target", string(p.target), "stage", string(p.stage), "code", string(p.code), "http_status", p.status, "duration_ms", elapsed.Milliseconds())
	}
}

func (p *probeDiagnostic) network(err error) {
	p.code = devlogs.CodeNetwork
	var networkError net.Error
	var dnsError *net.DNSError
	if errors.As(err, &networkError) && networkError.Timeout() {
		p.code = devlogs.CodeTimeout
	} else if errors.As(err, &dnsError) {
		p.code = devlogs.CodeDNS
	}
}
