package postgres

import (
	"context"
	"errors"
	"testing"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

func TestSetupStatusRepositoryFailsClosedWithoutPool(t *testing.T) {
	tests := []struct {
		name       string
		repository *SetupStatusRepository
	}{
		{name: "nil repository"},
		{name: "nil pool", repository: NewSetupStatusRepository(nil)},
		{name: "empty pool", repository: NewSetupStatusRepository(&Pool{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.repository.ReadState(context.Background()); !errors.Is(err, setup.ErrStatusUnavailable) {
				t.Fatalf("ReadState() error = %v, want %v", err, setup.ErrStatusUnavailable)
			}
		})
	}
}

func TestSetupStatusRepositoryHonorsPreCanceledContextWithoutPoolAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewSetupStatusRepository(nil).ReadState(ctx); !errors.Is(err, setup.ErrStatusCanceled) {
		t.Fatalf("ReadState() error = %v, want %v", err, setup.ErrStatusCanceled)
	}
}
