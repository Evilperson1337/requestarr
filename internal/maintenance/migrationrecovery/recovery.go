// Package migrationrecovery defines the deliberately narrow operator boundary
// for the certified dirty-v20 migration recovery procedure.
package migrationrecovery

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
)

const confirmationPhrase = "certify-dirty-v20-recovery"

var ErrConfirmationRequired = errors.New("recovery requires the exact --confirm phrase")

// Request is intentionally unparameterized: this maintenance operation cannot
// be repurposed into a generic migration-force command.
type Request struct{}

// ParseCommand accepts only the certified dirty-v20 recovery subcommand and
// its exact, explicit confirmation.
func ParseCommand(args []string) (Request, error) {
	if len(args) == 0 || args[0] != "recover-dirty-v20" {
		return Request{}, errors.New("expected maintenance recover-dirty-v20")
	}

	flags := flag.NewFlagSet("recover-dirty-v20", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	confirmation := flags.String("confirm", "", "exact confirmation phrase")
	if err := flags.Parse(args[1:]); err != nil {
		return Request{}, errors.New("invalid dirty-v20 recovery arguments")
	}
	if flags.NArg() != 0 {
		return Request{}, errors.New("dirty-v20 recovery accepts no positional arguments")
	}
	if *confirmation != confirmationPhrase {
		return Request{}, ErrConfirmationRequired
	}
	return Request{}, nil
}

// Execute invokes only the injected certified recovery operation. It does not
// expose a version number or a generic force mechanism to command callers.
func Execute(ctx context.Context, _ Request, databaseURL string, recoverDirtyV20 func(context.Context, string) error) error {
	if err := ctx.Err(); err != nil {
		return errors.New("migration recovery canceled")
	}
	if err := recoverDirtyV20(ctx, databaseURL); err != nil {
		return fmt.Errorf("dirty-v20 recovery failed: %w", err)
	}
	return nil
}

// ConfirmationPhrase is surfaced only for command help and operator
// documentation; callers must still provide it explicitly.
func ConfirmationPhrase() string { return confirmationPhrase }
