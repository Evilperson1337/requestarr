package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"time"

	gomigrate "github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
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

var ErrDirtyV20RecoveryNotEligible = errors.New("migration recovery is not eligible for this database state")

// noLockDriver delegates all migration-driver work except locking. Recovery
// owns the PostgreSQL advisory lock before history inspection through replay.
type noLockDriver struct{ database.Driver }

func (noLockDriver) Lock() error   { return nil }
func (noLockDriver) Unlock() error { return nil }

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
	// Read history only after Up has acquired and released the migration lock.
	// Reading it before Up races with another migrator's transient dirty marker.
	current, dirty, versionErr := migrator.Version()
	if versionErr != nil && !errors.Is(versionErr, gomigrate.ErrNilVersion) {
		return errors.New("migration history read failed")
	}
	if dirty {
		return errors.New("migration history is dirty")
	}
	if versionErr == nil && current > latest {
		return fmt.Errorf("migration history version is newer than this binary")
	}
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

// RecoverDirtyV20 certifies the known failed-v20 physical state, resets
// truthful history to clean v19, then replays the ordinary forward-only chain.
func RecoverDirtyV20(ctx context.Context, databaseURL string) error {
	if err := ctx.Err(); err != nil {
		return errors.New("migration recovery canceled")
	}

	pgxConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("migration database configuration invalid")
	}
	pgxConfig.RuntimeParams["application_name"] = "requestarr-dirty-v20-recovery"
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
	driver, err := migratepostgres.WithConnection(connectionCtx, conn, &migratepostgres.Config{DatabaseName: pgxConfig.Database, StatementTimeout: statementTimeout})
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

	if err := driver.Lock(); err != nil {
		if ctx.Err() != nil {
			return errors.New("migration recovery canceled")
		}
		return errors.New("migration recovery lock timeout")
	}
	defer func() { _ = driver.Unlock() }()

	current, dirty, historyErr := migrator.Version()
	if historyErr != nil && !errors.Is(historyErr, gomigrate.ErrNilVersion) {
		return errors.New("migration recovery history read failed")
	}
	if historyErr != nil || current != 20 || !dirty {
		return ErrDirtyV20RecoveryNotEligible
	}
	if err := certifyFailedV20State(ctx, conn); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.New("migration recovery canceled")
	}
	if err := driver.SetVersion(19, false); err != nil {
		return errors.New("migration recovery history reset failed")
	}
	lockedMigrator, err := gomigrate.NewWithInstance("iofs", source, pgxConfig.Database, noLockDriver{Driver: driver})
	if err != nil {
		return errors.New("migration recovery runner initialization failed")
	}
	lockedMigrator.LockTimeout = lockTimeout
	err = lockedMigrator.Up()
	if err == nil || errors.Is(err, gomigrate.ErrNoChange) {
		return nil
	}
	if ctx.Err() != nil {
		return errors.New("migration recovery canceled")
	}
	return errors.New("migration recovery replay failed")
}

// certifyFailedV20State verifies that v20 created no physical objects and v19
// integrity validation still accepts all pre-existing evidence. SQL diagnostics
// are deliberately not returned to avoid leaking database contents.
func certifyFailedV20State(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `DO $$
DECLARE evidence RECORD;
BEGIN
    IF to_regclass('inventory_source_configuration_credential_revisions') IS NOT NULL
       OR to_regprocedure('lock_inventory_source_configuration_write()') IS NOT NULL
       OR to_regprocedure('assert_inventory_source_configuration_credential_envelope(uuid)') IS NOT NULL THEN
        RAISE EXCEPTION 'unexpected v20 physical object';
    END IF;
    IF to_regprocedure('assert_inventory_source_configuration_projection(uuid)') IS NULL
       OR to_regprocedure('assert_inventory_source_configuration_revision_evidence(uuid,bigint)') IS NULL
       OR to_regprocedure('enforce_inventory_source_configuration_integrity()') IS NULL THEN
        RAISE EXCEPTION 'v19 integrity functions unavailable';
    END IF;
    FOR evidence IN SELECT source_id FROM inventory_source_configurations LOOP
        PERFORM assert_inventory_source_configuration_projection(evidence.source_id);
    END LOOP;
    FOR evidence IN SELECT source_id, revision FROM inventory_source_configuration_revisions LOOP
        PERFORM assert_inventory_source_configuration_revision_evidence(evidence.source_id, evidence.revision);
    END LOOP;
END $$`)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("migration recovery canceled")
		}
		return errors.New("migration recovery physical certification failed")
	}
	return nil
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
