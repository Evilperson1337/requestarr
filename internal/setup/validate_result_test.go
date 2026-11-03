package setup

import (
	"context"
	"testing"
)

// Validate records what the checker says and normalizes unknown codes; the
// wording itself belongs to the checker (see checks/messages_test.go).
func TestValidateRecordsTheCheckersMessageAndNormalizesUnknownCodes(t *testing.T) {
	actor := Actor{ID: "admin-1", Role: "admin"}
	for _, tc := range []struct {
		code      CheckCode
		want      CheckCode
		wantValid bool
	}{
		{CheckOK, CheckOK, true},
		{CheckDisabled, CheckDisabled, true},
		{CheckAuthentication, CheckAuthentication, false},
		{CheckEnvironment, CheckEnvironment, false},
		{CheckIncomplete, CheckIncomplete, false},
		{CheckCode("something_new"), CheckUnavailable, false},
	} {
		svc := NewService(&memoryDrafts{}, codeChecker{tc.code}, Runtime{})
		draft, err := svc.Load(context.Background(), actor)
		if err != nil {
			t.Fatal(err)
		}
		draft, err = svc.Validate(context.Background(), actor, draft.Revision, TargetTMDB)
		if err != nil {
			t.Fatal(err)
		}
		got := draft.Validations[len(draft.Validations)-1]
		if got.Code != tc.want || got.Valid != tc.wantValid || got.Message != "described: "+string(tc.want) {
			t.Errorf("%s: %+v", tc.code, got)
		}
	}
}

// A saved IGDB client ID applies at restart; it is not a missing environment variable.
func TestSavedIGDBClientIDIsNotListedAsMissingEnvironment(t *testing.T) {
	svc := NewService(&memoryDrafts{}, codeChecker{CheckEnvironment}, Runtime{})
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, err := svc.Load(context.Background(), actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range draft.Environment {
		if item.Name == "REQUESTARR_IGDB_CLIENT_ID" {
			t.Fatal("saved client ID incorrectly reported as a missing environment variable")
		}
	}
}
