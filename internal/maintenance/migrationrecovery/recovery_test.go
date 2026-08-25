package migrationrecovery

import (
	"context"
	"errors"
	"testing"
)

func TestParseCommandRequiresExactCertifiedRecoveryBoundary(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want error
	}{
		{name: "accepts certified command", args: []string{"recover-dirty-v20", "--confirm", confirmationPhrase}},
		{name: "rejects another maintenance operation", args: []string{"force", "--confirm", confirmationPhrase}},
		{name: "rejects missing confirmation", args: []string{"recover-dirty-v20"}, want: ErrConfirmationRequired},
		{name: "rejects near confirmation", args: []string{"recover-dirty-v20", "--confirm", "certify-dirty-v21-recovery"}, want: ErrConfirmationRequired},
		{name: "rejects positional version", args: []string{"recover-dirty-v20", "--confirm", confirmationPhrase, "20"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCommand(tt.args)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("ParseCommand(%q) error = %v, want %v", tt.args, err, tt.want)
				}
				return
			}
			if (err != nil) != (tt.name != "accepts certified command") {
				t.Fatalf("ParseCommand(%q) error = %v", tt.args, err)
			}
		})
	}
}

func TestExecuteHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Execute(ctx, Request{}, "postgres://secret", func(context.Context, string) error {
		called = true
		return nil
	})
	if err == nil || err.Error() != "migration recovery canceled" {
		t.Fatalf("Execute() error = %v, want canceled recovery", err)
	}
	if called {
		t.Fatal("Execute invoked recovery after cancellation")
	}
}
