package password

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

var fast = Hasher{Iterations: 1000}

func TestHashThenVerify(t *testing.T) {
	encoded, err := fast.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !fast.Verify("correct horse battery", encoded) {
		t.Fatal("the right password was rejected")
	}
	for _, wrong := range []string{"", "correct horse batter", "Correct horse battery", "correct horse battery "} {
		if fast.Verify(wrong, encoded) {
			t.Fatalf("%q was accepted", wrong)
		}
	}
}

func TestHashCarriesSchemeIterationsAndSaltAndNeverThePassword(t *testing.T) {
	a, _ := fast.Hash("same password value")
	b, _ := fast.Hash("same password value")
	if a == b {
		t.Fatal("two hashes of one password are identical: the salt is not random")
	}
	parts := strings.Split(a, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "1000" {
		t.Fatalf("unexpected encoding %q", a)
	}
	if salt, err := base64.RawStdEncoding.DecodeString(parts[2]); err != nil || len(salt) < 16 {
		t.Fatalf("salt must decode to at least 16 bytes: %v", err)
	}
	if strings.Contains(a, "same password value") {
		t.Fatal("hash contains the password")
	}
}

func TestDefaultIterationsMeetCurrentOWASPGuidance(t *testing.T) {
	if Default.Iterations < 600_000 {
		t.Fatalf("PBKDF2-HMAC-SHA256 needs at least 600000 iterations, got %d", Default.Iterations)
	}
}

func TestVerifyRejectsMalformedAndHostileEncodings(t *testing.T) {
	good, _ := fast.Hash("whatever password")
	salt := strings.Split(good, "$")[2]
	sum := base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size))
	for name, encoded := range map[string]string{
		"empty":              "",
		"garbage":            "not a hash",
		"unknown scheme":     "md5$1000$" + salt + "$" + sum,
		"zero iterations":    "pbkdf2-sha256$0$" + salt + "$" + sum,
		"negative":           "pbkdf2-sha256$-5$" + salt + "$" + sum,
		"absurd iterations":  "pbkdf2-sha256$2000000000$" + salt + "$" + sum,
		"not a number":       "pbkdf2-sha256$many$" + salt + "$" + sum,
		"bad salt":           "pbkdf2-sha256$1000$!!!$" + sum,
		"empty salt":         "pbkdf2-sha256$1000$$" + sum,
		"bad sum":            "pbkdf2-sha256$1000$" + salt + "$!!!",
		"truncated sum":      "pbkdf2-sha256$1000$" + salt + "$AAAA",
		"extra part":         good + "$x",
		"missing part":       "pbkdf2-sha256$1000$" + salt,
		"empty derived hash": "pbkdf2-sha256$1000$" + salt + "$",
	} {
		if fast.Verify("whatever password", encoded) {
			t.Errorf("%s: malformed encoding verified", name)
		}
		if !fast.NeedsRehash(encoded) {
			t.Errorf("%s: malformed encoding should need a rehash", name)
		}
	}
}

func TestNeedsRehashWhenWeakerThanCurrentSettings(t *testing.T) {
	old, _ := Hasher{Iterations: 1000}.Hash("pw pw pw pw pw")
	if fast.NeedsRehash(old) {
		t.Error("a hash at the current iteration count must not need a rehash")
	}
	if !(Hasher{Iterations: 2000}).NeedsRehash(old) {
		t.Error("a hash below the current iteration count must need a rehash")
	}
	if !(Hasher{Iterations: 2000}).Verify("pw pw pw pw pw", old) {
		t.Error("an old hash must still verify after the iteration count is raised")
	}
}

func TestZeroHasherUsesTheDefault(t *testing.T) {
	var zero Hasher
	if got := zero.iterations(); got != Default.Iterations {
		t.Fatalf("zero value uses %d iterations, want %d", got, Default.Iterations)
	}
}

func TestRandomPasswordsAreLongUniqueAndUnambiguous(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		p, err := Random()
		if err != nil {
			t.Fatal(err)
		}
		if utf8.RuneCountInString(p) < 20 {
			t.Fatalf("%q is shorter than 20 characters", p)
		}
		if strings.ContainsAny(p, "0O1lI|`'\" \\/") {
			t.Fatalf("%q contains an ambiguous or awkward character", p)
		}
		if seen[p] {
			t.Fatal("random password repeated")
		}
		seen[p] = true
	}
}
