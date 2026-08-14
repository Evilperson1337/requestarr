package migrations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	gomigrate "github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	embedded "git.example.com/evilperson1337/Requestarr/db/migrations"
)

const (
	connectionTimeout = 10 * time.Second
	lockTimeout       = 15 * time.Second
	statementTimeout  = 60 * time.Second
)

// Up applies all pending embedded migrations. It never migrates down or repairs
// dirty history automatically.
func Up(ctx context.Context, databaseURL string) error {
	if err := ctx.Err(); err != nil {
		return errors.New("migration canceled")
	}

	pgxConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("migration database configuration invalid")
	}
	pgxConfig.RuntimeParams["application_name"] = "requestarr-migrations"
	// PostgreSQL bounds the driver's blocking advisory-lock statement. The
	// migrate-level timeout remains a second guard around lock acquisition.
	pgxConfig.RuntimeParams["lock_timeout"] = lockTimeout.String()

	db := stdlib.OpenDB(*pgxConfig)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	connectionCtx, cancelConnection := context.WithTimeout(ctx, connectionTimeout)
	defer cancelConnection()
	conn, err := db.Conn(connectionCtx)
	if err != nil {
		return errors.New("migration connection failed")
	}

	driver, err := migratepostgres.WithConnection(connectionCtx, conn, &migratepostgres.Config{
		DatabaseName:     pgxConfig.Database,
		StatementTimeout: statementTimeout,
	})
	if err != nil {
		_ = conn.Close()
		return errors.New("migration driver initialization failed")
	}

	source, err := iofs.New(embedded.Files, ".")
	if err != nil {
		_ = driver.Close()
		return errors.New("embedded migration source invalid")
	}
	migrator, err := gomigrate.NewWithInstance("iofs", source, pgxConfig.Database, driver)
	if err != nil {
		_ = source.Close()
		_ = driver.Close()
		return errors.New("migration runner initialization failed")
	}
	migrator.LockTimeout = lockTimeout
	defer closeMigrator(migrator)

	latest, err := latestVersion(source)
	if err != nil {
		return errors.New("embedded migration source has no versions")
	}
	current, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, gomigrate.ErrNilVersion) {
		return errors.New("migration history read failed")
	}
	if dirty {
		return errors.New("migration history is dirty")
	}
	if err == nil && current > latest {
		return fmt.Errorf("migration history version is newer than this binary")
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case migrator.GracefulStop <- true:
			default:
			}
		case <-stop:
		}
	}()
	err = migrator.Up()
	close(stop)
	if err == nil || errors.Is(err, gomigrate.ErrNoChange) {
		return nil
	}
	if errors.Is(err, gomigrate.ErrLockTimeout) {
		return errors.New("migration lock timeout")
	}
	if ctx.Err() != nil {
		return errors.New("migration canceled")
	}
	return errors.New("migration execution failed")
}

func latestVersion(source source.Driver) (uint, error) {
	version, err := source.First()
	if err != nil {
		return 0, err
	}
	for {
		next, err := source.Next(version)
		if errors.Is(err, fs.ErrNotExist) {
			return version, nil
		}
		if err != nil {
			return 0, err
		}
		version = next
	}
}

func closeMigrator(migrator *gomigrate.Migrate) {
	_, _ = migrator.Close()
}
