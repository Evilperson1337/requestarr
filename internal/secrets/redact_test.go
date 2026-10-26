package secrets

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestPlaintextHoldersRedactUnderFormattingAndLogging(t *testing.T) {
	const probe = "PROBE-plaintext-77"
	r := Resolved{Value: probe, Source: SourceStored}
	svc := NewService(nil, nil, map[Name]string{TMDBReadToken: probe})
	for name, v := range map[string]any{"resolved": r, "resolvedPtr": &r, "service": svc} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		logger.Info("x", slog.Any("v", v))
		for _, out := range []string{fmt.Sprintf("%v", v), fmt.Sprintf("%+v", v), fmt.Sprintf("%#v", v), fmt.Sprintf("%s", v), logs.String()} {
			if strings.Contains(out, probe) {
				t.Errorf("%s leaks value: %s", name, out)
			}
		}
	}
	if r.Value != probe {
		t.Fatal("redaction must not change the value")
	}
}
