package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

// Production code takes explicit Deps. These constructors let a test hand over
// one fake "backend" that implements only the capabilities the test cares
// about; every other dependency is an absent stub that fails when used. The
// type assertions live here, in test code, and nowhere in the router.

var errAbsent = errors.New("dependency absent in this test")

type absentRepo struct{}

func (absentRepo) CreateOrGet(context.Context, domain.Request, domain.RequestSubscription) (domain.Request, bool, error) {
	return domain.Request{}, false, errAbsent
}
func (absentRepo) List(context.Context, string, requests.ListQuery) (requests.Page, error) {
	return requests.Page{}, errAbsent
}
func (absentRepo) CatalogMediaType(context.Context, string) (domain.MediaType, error) {
	return "", errAbsent
}
func (absentRepo) ValidatePlatforms(context.Context, string, domain.PlatformSelection) error {
	return errAbsent
}
func (absentRepo) CatalogPlatforms(context.Context, string) ([]domain.Platform, error) {
	return nil, errAbsent
}
func (absentRepo) Transition(context.Context, requests.TransitionInput) (domain.Request, bool, error) {
	return domain.Request{}, false, errAbsent
}
func (absentRepo) Subscribe(context.Context, string, string) (bool, error) { return false, errAbsent }
func (absentRepo) Unsubscribe(context.Context, string, string, bool) (bool, bool, error) {
	return false, false, errAbsent
}

type absentInbox struct{}

func (absentInbox) ListInbox(context.Context, string) ([]notifications.InboxItem, error) {
	return nil, errAbsent
}
func (absentInbox) MarkInboxRead(context.Context, string, string) (bool, error) {
	return false, errAbsent
}

type absentMatchReview struct{}

func (absentMatchReview) ListMatchSuggestions(context.Context) ([]domain.MatchSuggestion, error) {
	return nil, errAbsent
}
func (absentMatchReview) ReviewMatchSuggestion(context.Context, string, string, string) error {
	return errAbsent
}

type absentJobRuns struct{}

func (absentJobRuns) RecentRunsBySource(context.Context, int) ([]jobs.SourceRuns, error) {
	return nil, errAbsent
}

type testStatus struct {
	fn func(context.Context) (opstatus.Overview, error)
}

func (s testStatus) Overview(ctx context.Context) (opstatus.Overview, error) {
	if s.fn == nil {
		return opstatus.Overview{}, errAbsent
	}
	return s.fn(ctx)
}

type testUsers struct {
	list   func(context.Context) ([]postgres.AdminUser, error)
	change func(context.Context, string, string, string, time.Time) error
}

func (u testUsers) ListAdminUsers(ctx context.Context) ([]postgres.AdminUser, error) {
	if u.list == nil {
		return nil, errAbsent
	}
	return u.list(ctx)
}
func (u testUsers) ChangeUserRole(ctx context.Context, a, b, c string, at time.Time) error {
	if u.change == nil {
		return errAbsent
	}
	return u.change(ctx, a, b, c, at)
}

// absentSetupState reads as a fresh install.
type absentSetupState struct{}

func (absentSetupState) SetupComplete(context.Context) (bool, error)       { return false, nil }
func (absentSetupState) SetupWizardComplete(context.Context) (bool, error) { return false, nil }

type absentSyncer struct{}

func (absentSyncer) SyncNow(context.Context, string) (jobs.Run, error) { return jobs.Run{}, errAbsent }

type absentResolver struct{}

func (absentResolver) ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error) {
	return domain.ResolvedItem{}, errAbsent
}

// noSessionRepo never has a session, so every protected request is a 401.
type noSessionRepo struct{ statusAuthRepo }

func (noSessionRepo) GetSession(context.Context, []byte, time.Time) (auth.User, []byte, time.Time, error) {
	return auth.User{}, nil, time.Time{}, errors.New("no session")
}

// testDeps fills Deps from a single fake backend by capability.
func testDeps(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, resolver Resolver, syncer SourceSyncer, wizard SetupService, authHandlers ...*auth.Handler) Deps {
	d := Deps{Logger: logger, Build: info, Readiness: readiness, Resolver: resolver, Syncer: syncer, Wizard: wizard}
	if len(authHandlers) > 0 && authHandlers[0] != nil {
		d.Auth = authHandlers[0]
	} else {
		d.Auth = &auth.Handler{Repo: noSessionRepo{}, Secret: "test-secret"}
	}
	if d.Resolver == nil {
		d.Resolver = absentResolver{}
	}
	if d.Syncer == nil {
		d.Syncer = absentSyncer{}
	}
	if d.Wizard == nil {
		d.Wizard = absentWizard{}
	}
	if repo, ok := readiness.(requests.Repository); ok {
		d.Requests = requests.New(repo)
	} else {
		d.Requests = requests.New(absentRepo{})
	}
	if v, ok := readiness.(notifications.InboxRepository); ok {
		d.Inbox = v
	} else {
		d.Inbox = absentInbox{}
	}
	if v, ok := readiness.(MatchReviewStore); ok {
		d.MatchReview = v
	} else {
		d.MatchReview = absentMatchReview{}
	}
	if v, ok := readiness.(JobRuns); ok {
		d.JobRuns = v
	} else {
		d.JobRuns = absentJobRuns{}
	}
	if v, ok := readiness.(interface {
		OperationalStatus(context.Context) (opstatus.Overview, error)
	}); ok {
		d.Status = testStatus{fn: v.OperationalStatus}
	} else {
		d.Status = testStatus{}
	}
	users := testUsers{}
	if v, ok := readiness.(interface {
		ListAdminUsers(context.Context) ([]postgres.AdminUser, error)
	}); ok {
		users.list = v.ListAdminUsers
	}
	if v, ok := readiness.(interface {
		ChangeUserRole(context.Context, string, string, string, time.Time) error
	}); ok {
		users.change = v.ChangeUserRole
	}
	d.Users = users
	if v, ok := readiness.(SetupState); ok {
		d.SetupState = v
	} else {
		d.SetupState = absentSetupState{}
	}
	if v, ok := wizard.(SecretsService); ok {
		d.Secrets = v
	} else {
		d.Secrets = absentSecrets{}
	}
	if v, ok := readiness.(DevelopmentLogsSource); ok {
		d.DevelopmentLogs = v.DevelopmentLogs()
	}
	return d
}

// DevelopmentLogsSource lets a fake backend supply the development recorder.
type DevelopmentLogsSource interface{ DevelopmentLogs() *devlogs.Recorder }

func mustRouter(d Deps) http.Handler {
	handler, _, err := buildRouter(d)
	if err != nil {
		panic(err)
	}
	return handler
}

func New(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, authHandlers ...*auth.Handler) http.Handler {
	return mustRouter(testDeps(logger, info, readiness, nil, nil, nil, authHandlers...))
}

func NewWithMetadata(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, resolver Resolver, authHandlers ...*auth.Handler) http.Handler {
	return mustRouter(testDeps(logger, info, readiness, resolver, nil, nil, authHandlers...))
}

func NewWithSync(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, resolver Resolver, syncer SourceSyncer, authHandlers ...*auth.Handler) http.Handler {
	return mustRouter(testDeps(logger, info, readiness, resolver, syncer, nil, authHandlers...))
}

func NewWithSetup(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, resolver Resolver, syncer SourceSyncer, wizard SetupService, authHandlers ...*auth.Handler) http.Handler {
	return mustRouter(testDeps(logger, info, readiness, resolver, syncer, wizard, authHandlers...))
}

// newRouter returns the handler and its route table for the policy tests.
func newRouter(logger *slog.Logger, info buildinfo.Info, readiness ReadinessChecker, resolver Resolver, syncer SourceSyncer, wizard SetupService, authHandlers ...*auth.Handler) (http.Handler, *routeTable) {
	h, rt, err := buildRouter(testDeps(logger, info, readiness, resolver, syncer, wizard, authHandlers...))
	if err != nil {
		panic(err)
	}
	return h, rt
}

type absentWizard struct{}

func (absentWizard) Load(context.Context, setup.Actor) (setup.Draft, error) {
	return setup.Draft{}, errAbsent
}
func (absentWizard) Save(context.Context, setup.Actor, setup.SaveInput) (setup.Draft, error) {
	return setup.Draft{}, errAbsent
}
func (absentWizard) Validate(context.Context, setup.Actor, int64, setup.Target) (setup.Draft, error) {
	return setup.Draft{}, errAbsent
}
func (absentWizard) Finish(context.Context, setup.Actor, int64) (setup.Draft, error) {
	return setup.Draft{}, errAbsent
}

type absentSecrets struct{}

func (absentSecrets) Status(context.Context) ([]secrets.Status, bool, error) {
	return nil, false, errAbsent
}
func (absentSecrets) Set(context.Context, string, secrets.Name, string) error { return errAbsent }
func (absentSecrets) Clear(context.Context, string, secrets.Name) error       { return errAbsent }
