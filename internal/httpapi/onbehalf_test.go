package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

const (
	behalfPerson  = "11111111-1111-4111-8111-111111111111"
	behalfCatalog = "22222222-2222-4222-8222-222222222222"
	behalfUnknown = "33333333-3333-4333-8333-333333333333"
)

type behalfBackend struct {
	requests.Repository
	created  []domain.Request
	subs     []domain.RequestSubscription
	roles    map[string]string
	existing domain.Request
}

func (b *behalfBackend) UserRole(_ context.Context, id string) (string, bool, error) {
	role, ok := b.roles[id]
	return role, ok, nil
}
func (b *behalfBackend) CatalogMediaType(context.Context, string) (domain.MediaType, error) {
	return domain.MediaMovie, nil
}
func (b *behalfBackend) ValidatePlatforms(context.Context, string, domain.PlatformSelection) error {
	return nil
}
func (b *behalfBackend) CreateOrGet(_ context.Context, r domain.Request, s domain.RequestSubscription) (domain.Request, bool, error) {
	if b.existing.ID != "" {
		return b.existing, false, nil
	}
	b.created, b.subs = append(b.created, r), append(b.subs, s)
	r.ID = "new-request"
	return r, true, nil
}
func (b *behalfBackend) Subscribe(_ context.Context, requestID, userID string) (bool, error) {
	b.subs = append(b.subs, domain.RequestSubscription{UserID: userID, Source: domain.SubscriptionManual})
	return true, nil
}

func behalfCall(t *testing.T, b *behalfBackend, role, person string) (code int, body string) {
	t.Helper()
	payload := `{"catalogId":"` + behalfCatalog + `","onBehalfOf":"` + person + `","platforms":{"any":true}}`
	rec := approvalCall(t, &approvalBackend{Repository: b}, role, http.MethodPost, "/requests", payload)
	return rec.Code, rec.Body.String()
}

func TestCreateRequestOnBehalfIsRefusedForRolesThatCannotManageRequests(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		b := &behalfBackend{roles: map[string]string{behalfPerson: "requester"}}
		if code, body := behalfCall(t, b, role, behalfPerson); code != http.StatusForbidden || len(b.created) != 0 {
			t.Errorf("%s: status=%d created=%d body=%s, want 403 and nothing created", role, code, len(b.created), body)
		}
	}
}

func TestCreateRequestOnBehalfMakesThePersonTheRequesterForAdminAndSuperuser(t *testing.T) {
	for _, role := range []string{"admin", "superuser"} {
		b := &behalfBackend{roles: map[string]string{behalfPerson: "requester"}}
		code, body := behalfCall(t, b, role, behalfPerson)
		if code != http.StatusCreated || len(b.created) != 1 {
			t.Fatalf("%s: status=%d created=%d body=%s", role, code, len(b.created), body)
		}
		if got := b.created[0]; got.RequestedBy != behalfPerson || got.MadeOnBehalfBy != "u-1" {
			t.Errorf("%s: request = %#v", role, got)
		}
		if b.subs[0].UserID != behalfPerson || b.subs[0].Source != domain.SubscriptionRequester {
			t.Errorf("%s: subscription = %#v", role, b.subs[0])
		}
	}
}

func TestCreateRequestOnBehalfRejectsABadPerson(t *testing.T) {
	b := &behalfBackend{roles: map[string]string{behalfPerson: "viewer"}}
	for name, person := range map[string]string{"not a uuid": "not-a-uuid", "a viewer cannot request": behalfPerson, "unknown person": behalfUnknown} {
		code, body := behalfCall(t, b, "admin", person)
		if code != http.StatusBadRequest || !strings.Contains(body, "invalid_request") || len(b.created) != 0 {
			t.Errorf("%s: status=%d body=%s", name, code, body)
		}
	}
}

func TestCreateRequestOnBehalfSubscribesThePersonToAnExistingRequest(t *testing.T) {
	b := &behalfBackend{roles: map[string]string{behalfPerson: "requester"}, existing: domain.Request{ID: "existing", Status: domain.RequestRequested}}
	code, body := behalfCall(t, b, "admin", behalfPerson)
	if code != http.StatusOK || len(b.subs) != 1 || b.subs[0].UserID != behalfPerson {
		t.Fatalf("status=%d subs=%#v body=%s", code, b.subs, body)
	}
}
