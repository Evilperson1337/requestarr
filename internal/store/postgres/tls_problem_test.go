package postgres

import (
	"crypto/x509"
	"errors"
	"fmt"
	"testing"
)

func TestTLSProblemIsRecognisedByTypeBeforeText(t *testing.T) {
	typed := fmt.Errorf("failed to connect: %w", x509.UnknownAuthorityError{})
	if !isTLSProblem(typed) {
		t.Fatal("an unknown certificate authority is a TLS problem")
	}
	if isTLSProblem(errors.New("password authentication mentions a table named tlsdata")) {
		t.Fatal("a stray 'tls' inside another word must not read as a TLS problem")
	}
}
