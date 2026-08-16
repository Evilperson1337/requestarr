//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	postgresmigrations "git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

var encryptedTestSchemaPattern = regexp.MustCompile(`^requestarr_test_[0-9a-f]{32}$`)

func TestEncryptedSecretRepositoryLifecycleAndTransactionRollback(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	cipher := secrets.New([32]byte{4})
	keyID := cipher.KeyID()
	repo := NewEncryptedSecretRepository(pool, &keyID)
	if err := repo.EstablishActiveKey(ctx); err != nil {
		t.Fatalf("establish key: %v", err)
	}
	if err := repo.EstablishActiveKey(ctx); err != nil {
		t.Fatalf("matching restart: %v", err)
	}
	semantic := secrets.Context{Namespace: "oidc", OwnerID: secrets.OwnerID{1}, Name: "client_secret"}
	envelope, err := cipher.Encrypt(semantic, []byte("database-plaintext-marker"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, semantic, envelope); err != nil {
		t.Fatal(err)
	}
	firstUpdated := readUpdatedAt(t, ctx, pool, semantic)
	time.Sleep(time.Millisecond)
	if err := repo.Upsert(ctx, semantic, envelope); err != nil {
		t.Fatal(err)
	}
	if updated := readUpdatedAt(t, ctx, pool, semantic); updated.Before(firstUpdated) {
		t.Fatalf("updated_at moved backwards: %v before %v", updated, firstUpdated)
	}
	got, err := repo.Read(ctx, semantic)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := cipher.Decrypt(semantic, got)
	if err != nil || string(plaintext) != "database-plaintext-marker" {
		t.Fatalf("decrypt=%q %v", plaintext, err)
	}

	rollbackContext := secrets.Context{Namespace: "oidc", OwnerID: secrets.OwnerID{2}, Name: "client_secret"}
	rollbackEnvelope, err := cipher.Encrypt(rollbackContext, []byte("rollback"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTx(ctx, tx, rollbackContext, rollbackEnvelope); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Read(ctx, rollbackContext); !errors.Is(err, ErrEncryptedSecretNotFound) {
		t.Fatalf("rolled-back read=%v", err)
	}

	wrongCipher := secrets.New([32]byte{5})
	wrongEnvelope, err := wrongCipher.Encrypt(semantic, []byte("wrong"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, semantic, wrongEnvelope); !errors.Is(err, ErrEncryptedSecretInvalid) {
		t.Fatalf("wrong envelope upsert=%v", err)
	}
	wrongID := wrongCipher.KeyID()
	if err := NewEncryptedSecretRepository(pool, &wrongID).EstablishActiveKey(ctx); !errors.Is(err, ErrEncryptionKeyIncompatible) {
		t.Fatalf("wrong restart=%v", err)
	}
	if err := NewEncryptedSecretRepository(pool, nil).EstablishActiveKey(ctx); !errors.Is(err, ErrEncryptionKeyRequired) {
		t.Fatalf("missing restart=%v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := repo.EstablishActiveKey(canceled); !errors.Is(err, ErrEncryptedSecretCanceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestEncryptedSecretRepositoryConcurrentFirstKeyAndUpserts(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	ids := []secrets.KeyID{secrets.New([32]byte{1}).KeyID(), secrets.New([32]byte{2}).KeyID()}
	errorsFound := make([]error, len(ids))
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errorsFound[i] = NewEncryptedSecretRepository(pool, &ids[i]).EstablishActiveKey(ctx)
		}(i)
	}
	wg.Wait()
	successes := 0
	winning := -1
	for i, err := range errorsFound {
		if err == nil {
			successes++
			winning = i
		} else if !errors.Is(err, ErrEncryptionKeyIncompatible) {
			t.Fatalf("unexpected establishment error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("first-key successes=%d errors=%v", successes, errorsFound)
	}

	cipher := secrets.New([32]byte{byte(winning + 1)})
	repo := NewEncryptedSecretRepository(pool, &ids[winning])
	semantic := secrets.Context{Namespace: "oidc", OwnerID: secrets.OwnerID{9}, Name: "client_secret"}
	envelope, err := cipher.Encrypt(semantic, []byte("concurrent"))
	if err != nil {
		t.Fatal(err)
	}
	errorsFound = make([]error, 12)
	for i := range errorsFound {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errorsFound[i] = repo.Upsert(ctx, semantic, envelope) }(i)
	}
	wg.Wait()
	for _, err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent upsert: %v", err)
		}
	}
	var count int
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM encrypted_secrets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count=%d err=%v", count, err)
	}
}

func TestEncryptedSecretRepositoryUpsertTxRequiresRegisteredMatchingKey(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()

	registeredCipher := secrets.New([32]byte{3})
	registeredID := registeredCipher.KeyID()
	writeCipher := secrets.New([32]byte{4})
	writeID := writeCipher.KeyID()
	semantic := secrets.Context{Namespace: "oidc", OwnerID: secrets.OwnerID{7}, Name: "client_secret"}
	envelope, err := writeCipher.Encrypt(semantic, []byte("transactional secret"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewEncryptedSecretRepository(pool, &writeID)

	for _, test := range []struct {
		name       string
		registered *secrets.KeyID
	}{
		{name: "registry absent"},
		{name: "registry key mismatches repository and envelope", registered: &registeredID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.pool.Exec(ctx, `DELETE FROM encryption_key_registry`); err != nil {
				t.Fatal(err)
			}
			if test.registered != nil {
				if _, err := pool.pool.Exec(ctx, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES(TRUE,$1)`, test.registered[:]); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.UpsertTx(ctx, tx, semantic, envelope); !errors.Is(err, ErrEncryptionKeyIncompatible) {
				_ = tx.Rollback(ctx)
				t.Fatalf("UpsertTx() error = %v, want ErrEncryptionKeyIncompatible", err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM encrypted_secrets`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("encrypted row count = %d, err %v; want zero", count, err)
			}
		})
	}
}

func TestEncryptedSecretDatabaseConstraintsRejectMalformedRows(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	keyID := secrets.New([32]byte{7}).KeyID()
	if err := NewEncryptedSecretRepository(pool, &keyID).EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	base := `INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,created_at,updated_at)
		VALUES ('oidc','00000000-0000-4000-8000-000000000001','client_secret',$1,$2,$3,$4,transaction_timestamp(),transaction_timestamp())`
	tests := []struct {
		version                int
		key, nonce, ciphertext []byte
	}{
		{2, keyID[:], make([]byte, 12), make([]byte, 16)},
		{1, make([]byte, 16), make([]byte, 12), make([]byte, 16)},
		{1, keyID[:], make([]byte, 11), make([]byte, 16)},
		{1, keyID[:], make([]byte, 12), make([]byte, 15)},
	}
	for i, tt := range tests {
		if _, err := pool.pool.Exec(ctx, base, tt.version, tt.key, tt.nonce, tt.ciphertext); err == nil {
			t.Fatalf("malformed row %d was accepted", i)
		}
	}
}

func migratedEncryptedPool(t *testing.T, ctx context.Context, databaseURL string) *Pool {
	t.Helper()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func readUpdatedAt(t *testing.T, ctx context.Context, pool *Pool, semantic secrets.Context) time.Time {
	t.Helper()
	var result time.Time
	if err := pool.pool.QueryRow(ctx, `SELECT updated_at FROM encrypted_secrets WHERE namespace=$1 AND owner_id=$2 AND secret_name=$3`, semantic.Namespace, semantic.OwnerID[:], semantic.Name).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func encryptedTestSchema(t *testing.T) (string, func()) {
	t.Helper()
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(baseURL)
	config, configErr := pgxpool.ParseConfig(baseURL)
	if err != nil || configErr != nil || config.ConnConfig.Database == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("invalid test database URL")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "requestarr_test_" + hex.EncodeToString(random)
	if !encryptedTestSchemaPattern.MatchString(schema) {
		t.Fatal("unsafe generated schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if encryptedTestSchemaPattern.MatchString(schema) {
			_, _ = admin.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		}
		admin.Close()
	}
}
