package auth

import (
	"encoding/base64"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRolePermissions(t *testing.T) {
	tests := []struct {
		principal  Principal
		permission Permission
		want       bool
	}{
		{Principal{UserID: "user", Role: RoleViewer}, PermissionViewRequests, true},
		{Principal{UserID: "user", Role: RoleViewer}, PermissionCreateRequests, false},
		{Principal{UserID: "user", Role: RoleRequester}, PermissionCreateRequests, true},
		{Principal{UserID: "user", Role: RoleRequester}, PermissionReviewRequests, false},
		{Principal{UserID: "user", Role: RoleAdmin}, PermissionManageSettings, true},
		{Principal{UserID: "", Role: RoleAdmin}, PermissionManageSettings, false},
	}
	for _, test := range tests {
		if got := test.principal.Allows(test.permission); got != test.want {
			t.Errorf("%+v Allows(%q) = %t, want %t", test.principal, test.permission, got, test.want)
		}
	}
}

func TestOpaqueMaterialValidationAndDomainSeparatedDigests(t *testing.T) {
	raw := make([]byte, opaqueMaterialBytes)
	for i := range raw {
		raw[i] = byte(i)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	state, err := ParseState(value)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := ParseNonce(value)
	if err != nil {
		t.Fatal(err)
	}
	session, err := ParseSessionToken(value)
	if err != nil {
		t.Fatal(err)
	}
	if state.Digest() == nonce.Digest() || state.Digest() == session.Digest() {
		t.Fatal("opaque artifact digests must be domain separated")
	}
	for _, invalid := range []string{"", value + "=", strings.TrimSuffix(value, "A") + "!", base64.RawURLEncoding.EncodeToString(raw[:31])} {
		if _, err := ParseState(invalid); err == nil {
			t.Errorf("ParseState(%q) succeeded", invalid)
		}
	}
}

func TestPKCEVerifierValidation(t *testing.T) {
	valid := strings.Repeat("A", 43)
	verifier, err := ParsePKCEVerifier(valid)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.Digest() == Digest("state", valid) {
		t.Fatal("PKCE digest must have a dedicated domain")
	}
	for _, invalid := range []string{strings.Repeat("A", 42), strings.Repeat("A", 129), valid + "!", "with space" + strings.Repeat("A", 35)} {
		if _, err := ParsePKCEVerifier(invalid); err == nil {
			t.Errorf("ParsePKCEVerifier(%q) succeeded", invalid)
		}
	}
}

func TestRepositoryDocumentsOneTimeTransactionAndLiveSessionContracts(t *testing.T) {
	fileSet := token.NewFileSet()
	_, fileName, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate auth package source")
	}
	file, err := parser.ParseFile(fileSet, filepath.Join(filepath.Dir(fileName), "persistence.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var repository *ast.InterfaceType
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == "Repository" {
				repository, _ = typeSpec.Type.(*ast.InterfaceType)
			}
		}
	}
	if repository == nil || repository.Incomplete {
		t.Fatal("Repository interface is missing or incomplete")
	}

	comments := map[string]string{}
	for _, field := range repository.Methods.List {
		if len(field.Names) == 1 && field.Doc != nil {
			comments[field.Names[0].Name] = field.Doc.Text()
		}
	}
	for method, required := range map[string][]string{
		"ConsumeLoginTransaction": {"atomic", "one-time-use"},
		"ReadSession":             {"live-session-only"},
	} {
		for _, phrase := range required {
			if !strings.Contains(comments[method], phrase) {
				t.Errorf("%s documentation must contain %q; got %q", method, phrase, comments[method])
			}
		}
	}
}
