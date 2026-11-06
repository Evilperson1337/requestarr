//go:build integration

package postgres

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
)

// S6: the current password is verified against the stored hash while the
// credential row is locked, in the same transaction as the update, so two
// concurrent changes cannot both succeed.
func TestOnlyOneOfTwoConcurrentPasswordChangesSucceedsIntegration(t *testing.T) {
	ctx, s, _, local := newManagedInstance(t)
	const current = "the current password of admin"
	currentHash, err := fastHasher.Hash(current)
	if err != nil {
		t.Fatal("hash failed")
	}
	if err := s.ChangeLocalPassword(ctx, local, currentHash, nil, "", time.Now(), nil); err != nil {
		t.Fatal("setup failed")
	}
	var wins atomic.Int32
	var refused atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, err := fastHasher.Hash("a brand new password number " + string(rune('a'+i)))
			if err != nil {
				t.Error("hash failed")
				return
			}
			err = s.ChangeLocalPassword(ctx, local, next, nil, "203.0.113.7", time.Now(), func(stored string) bool { return fastHasher.Verify(current, stored) })
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, auth.ErrCurrentPasswordIncorrect):
				refused.Add(1)
			default:
				t.Errorf("unexpected error %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || refused.Load() != 7 {
		t.Fatalf("wins=%d refused=%d, want exactly one change to succeed", wins.Load(), refused.Load())
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='password_changed' AND user_id=$1::uuid`, local); n != 2 { // the setup change plus the one winner
		t.Fatalf("password_changed rows=%d, want 2", n)
	}
}

func TestTheCurrentPasswordIsCheckedWhileTheCredentialRowIsLockedIntegration(t *testing.T) {
	ctx, s, _, local := newManagedInstance(t)
	next, err := fastHasher.Hash("a brand new password for admin")
	if err != nil {
		t.Fatal("hash failed")
	}
	var lockedDuringVerify bool
	err = s.ChangeLocalPassword(ctx, local, next, nil, "", time.Now(), func(string) bool {
		tx, berr := s.pool.Begin(ctx)
		if berr != nil {
			t.Error("probe failed")
			return true
		}
		defer tx.Rollback(ctx)
		_, lerr := tx.Exec(ctx, `SELECT 1 FROM local_credentials WHERE user_id=$1::uuid FOR UPDATE NOWAIT`, local)
		lockedDuringVerify = lerr != nil
		return true
	})
	if err != nil || !lockedDuringVerify {
		t.Fatalf("err=%v lockedDuringVerify=%v", err, lockedDuringVerify)
	}
}

func TestAWrongCurrentPasswordChangesNothingIntegration(t *testing.T) {
	ctx, s, _, local := newManagedInstance(t)
	before, err := s.LocalCredentialByUserID(ctx, local)
	if err != nil {
		t.Fatal("read failed")
	}
	err = s.ChangeLocalPassword(ctx, local, "pbkdf2-sha256$1$AAAA$AAAA", []byte("keep"), "203.0.113.7", time.Now(), func(string) bool { return false })
	if !errors.Is(err, auth.ErrCurrentPasswordIncorrect) {
		t.Fatalf("err=%v", err)
	}
	after, _ := s.LocalCredentialByUserID(ctx, local)
	if after.PasswordHash != before.PasswordHash || after.MustChange != before.MustChange {
		t.Fatal("a refused change altered the credential")
	}
}
