package postgres

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/evilperson1337/requestarr/db/migrations"
)

// runMigrations applies all pending embedded up migrations using the given
// pool. It reuses the pool's connections via database/sql rather than opening
// a second connection to PostgreSQL.
func runMigrations(pool *pgxpool.Pool) error {
	sourceDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	dbDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migration database driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	// dbDriver holds a *sql.Conn checked out from the pool for the lifetime of
	// its advisory lock; without closing m, that connection is never returned
	// and a later pool.Close() blocks forever waiting for it.
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
