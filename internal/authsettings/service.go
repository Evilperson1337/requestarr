package authsettings

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

var (
	// ErrNoOtherSignIn means the change would leave nobody able to sign in: the
	// local administrator is disabled, so OIDC is the only way in.
	ErrNoOtherSignIn = errors.New("the local administrator is disabled, so OIDC is the only way in; turn the local administrator back on before turning OIDC off")
	ErrReadOnly      = errors.New("OIDC is controlled by the environment; update the deployment or discard saved settings")
	ErrDiscovery     = errors.New("OIDC discovery failed; check issuer reachability and discovery metadata")
	ErrStorage       = errors.New("could not persist OIDC settings; previous configuration remains active")
	ErrInvalid       = errors.New("OIDC settings are invalid; supply an absolute issuer URL, client ID, client secret and provider name")
)

type Settings struct {
	Enabled      bool   `json:"enabled"`
	ProviderName string `json:"providerName"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
}
type Update struct {
	Settings
	// Omitted or empty retains the saved secret. Discard removes it.
	ClientSecret string `json:"clientSecret,omitempty"`
}

func (Update) String() string     { return "OIDC update [redacted]" }
func (u Update) GoString() string { return u.String() }

type Record struct {
	Settings Settings
	Secret   *secrets.Stored
}
type Repository interface {
	LoadOIDC(context.Context) (*Record, error)
	// SaveOIDC atomically replaces metadata, encrypted secret and audit. nil discards.
	SaveOIDC(context.Context, string, *Record) error
}
type Factory func(context.Context, Values, string) (oidc.Provider, error)

// Snapshot is immutable after publication. It binds the identity issuer to the provider.
type Snapshot struct {
	Provider             oidc.Provider
	Issuer, ProviderName string
}
type Options struct {
	OIDCEnabled  bool   `json:"oidcEnabled"`
	ProviderName string `json:"providerName"`
}
type Status struct {
	Resolution
	Saved            *Settings `json:"saved"`
	Effective        Settings  `json:"effective"`
	SecretConfigured bool      `json:"secretConfigured"`
	SecretUnreadable bool      `json:"secretUnreadable"`
	CanSaveSecret    bool      `json:"canSaveSecret"`
	ReadOnly         bool      `json:"readOnly"`
	RedirectURI      string    `json:"redirectUri"`
	Working          bool      `json:"working"`
	Problem          string    `json:"problem"`
}

type Service struct {
	// writeMu serialises the writers (Save, SetEnabled, Discard), which may hold
	// it across a network discovery. stateMu guards record and problem and is
	// only ever held briefly, never across I/O, so Status and the sign-in path
	// (which reads the atomic snapshot) never wait for a discovery.
	writeMu  sync.Mutex
	stateMu  sync.RWMutex
	cur      atomic.Pointer[Snapshot]
	repo     Repository
	sealer   secrets.Sealer
	env      Values
	redirect string
	factory  Factory
	record   *Record
	problem  string
	// localOff reports whether the local administrator is disabled. Nil means
	// there is no local sign-in to protect, so nothing is refused.
	localOff func(context.Context) (bool, error)
}

// RequireSignInPath makes every change that would turn OIDC off refuse while
// the local administrator is disabled, so a disabled recovery account cannot be
// combined with a switched-off provider to lock everyone out. A failure to read
// the local state refuses too: this is a safety check, so it fails closed.
func (s *Service) RequireSignInPath(localOff func(context.Context) (bool, error)) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.localOff = localOff
}

func (s *Service) keepASignInPath(ctx context.Context) error {
	if s.localOff == nil {
		return nil
	}
	disabled, err := s.localOff(ctx)
	if err != nil {
		return ErrStorage
	}
	if disabled {
		return ErrNoOtherSignIn
	}
	return nil
}

func (*Service) String() string     { return "OIDC service [redacted]" }
func (s *Service) GoString() string { return s.String() }

// New allows absent, incomplete, unreadable or temporarily unavailable OIDC at
// startup so that local recovery and Authentication settings remain reachable.
func New(ctx context.Context, repo Repository, sealer secrets.Sealer, env Values, publicURL string, factory Factory) (*Service, error) {
	s := &Service{repo: repo, sealer: sealer, env: env, redirect: strings.TrimRight(publicURL, "/") + "/api/v1/auth/callback", factory: factory}
	if factory == nil {
		s.factory = func(ctx context.Context, v Values, redirect string) (oidc.Provider, error) {
			return oidc.New(ctx, oidc.Config{Issuer: v.Issuer, ClientID: v.ClientID, ClientSecret: v.ClientSecret}, redirect)
		}
	}
	s.cur.Store(&Snapshot{})
	record, err := repo.LoadOIDC(ctx)
	if err != nil {
		return nil, ErrStorage
	}
	s.record = record
	v, enabled, name := s.effective()
	if enabled {
		p, err := s.discover(ctx, v)
		if err != nil {
			s.problem = err.Error()
		} else {
			s.cur.Store(&Snapshot{Provider: p, Issuer: v.Issuer, ProviderName: name})
		}
	}
	return s, nil
}
func (s *Service) Snapshot() *Snapshot { return s.cur.Load() }
func (s *Service) Options() Options {
	return Options{OIDCEnabled: s.OIDCWorking(), ProviderName: s.Snapshot().ProviderName}
}

// OIDCWorking reports whether OIDC is enabled and its provider is active. It is
// the one answer to "can someone sign in with OIDC" for the sign-in screen, the
// checklist and the lockout guards.
func (s *Service) OIDCWorking() bool { return s.Snapshot().Provider != nil }
func (s *Service) savedValues() (*Values, bool) {
	if s.record == nil {
		return nil, false
	}
	v := &Values{Issuer: s.record.Settings.Issuer, ClientID: s.record.Settings.ClientID}
	unreadable := false
	if s.record.Secret != nil {
		if s.sealer == nil {
			unreadable = true
		} else {
			plain, err := s.sealer.Open(secrets.OIDCClientSecret, s.record.Secret.Sealed)
			if err != nil {
				unreadable = true
			} else {
				v.ClientSecret = string(plain)
			}
		}
	}
	return v, unreadable
}
func (s *Service) effective() (Values, bool, string) {
	saved, _ := s.savedValues()
	r := Resolve(s.env, saved)
	if s.env != (Values{}) {
		return r.Effective, true, "OIDC"
	}
	if s.record != nil {
		return r.Effective, s.record.Settings.Enabled, s.record.Settings.ProviderName
	}
	return Values{}, false, ""
}
func (s *Service) Status(context.Context) (Status, error) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	saved, unreadable := s.savedValues()
	r := Resolve(s.env, saved)
	v, enabled, name := s.effective()
	st := Status{Resolution: r, Effective: Settings{Enabled: enabled, ProviderName: name, Issuer: v.Issuer, ClientID: v.ClientID}, SecretConfigured: v.ClientSecret != "", SecretUnreadable: unreadable, CanSaveSecret: s.sealer != nil, ReadOnly: s.env != (Values{}), RedirectURI: s.redirect, Working: s.OIDCWorking(), Problem: s.problem}
	if s.record != nil {
		copy := s.record.Settings
		st.Saved = &copy
	}
	return st, nil
}
func validText(v string, max int) bool {
	return utf8.ValidString(v) && len(v) <= max && strings.TrimSpace(v) == v && !strings.ContainsFunc(v, unicode.IsControl)
}
func valid(v Values, name string) bool {
	u, err := url.Parse(v.Issuer)
	return err == nil && len(v.Issuer) <= 2048 && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" && validText(v.Issuer, 2048) && v.ClientID != "" && validText(v.ClientID, 512) && v.ClientSecret != "" && validText(v.ClientSecret, 4096) && name != "" && validText(name, 100)
}
func (s *Service) discover(ctx context.Context, v Values) (oidc.Provider, error) {
	if !valid(v, "OIDC") {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := s.factory(ctx, v, s.redirect)
	if err != nil || p == nil {
		for _, safe := range []error{oidc.ErrIssuerURL, oidc.ErrDiscoveryDocument, oidc.ErrDiscoveryMetadata} {
			if errors.Is(err, safe) {
				return nil, errors.Join(ErrDiscovery, safe)
			}
		}
		return nil, ErrDiscovery
	}
	return p, nil
}
func (s *Service) candidate(u Update) (Values, error) {
	saved, unreadable := s.savedValues()
	secret := u.ClientSecret
	if secret == "" && saved != nil {
		secret = saved.ClientSecret
	}
	if secret == "" && unreadable {
		return Values{}, secrets.ErrUnreadable
	}
	v := Values{Issuer: u.Issuer, ClientID: u.ClientID, ClientSecret: secret}
	if !valid(v, u.ProviderName) {
		return Values{}, ErrInvalid
	}
	return v, nil
}

// Test performs discovery only; Save always re-tests its own exact candidate.
//
// It changes nothing, so it takes no writer lock, and it reads the saved state
// only briefly: the discovery itself runs with no lock held.
func (s *Service) Test(ctx context.Context, u Update) error {
	var v Values
	var err error
	if s.env != (Values{}) {
		v = s.env
	} else {
		s.stateMu.RLock()
		v, err = s.candidate(u)
		s.stateMu.RUnlock()
		if err != nil {
			return err
		}
	}
	_, err = s.discover(ctx, v)
	return err
}
func (s *Service) Save(ctx context.Context, actor string, u Update) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.save(ctx, actor, u)
}
func (s *Service) save(ctx context.Context, actor string, u Update) error {
	if s.env != (Values{}) {
		return ErrReadOnly
	}
	if u.ClientSecret != "" && s.sealer == nil {
		return secrets.ErrNoMasterKey
	}
	if !u.Enabled && s.OIDCWorking() {
		if err := s.keepASignInPath(ctx); err != nil {
			return err
		}
	}
	v, err := s.candidate(u)
	if err != nil {
		return err
	}
	var p oidc.Provider
	if u.Enabled {
		p, err = s.discover(ctx, v)
		if err != nil {
			return err
		}
	}
	previous, _ := s.savedValues()
	unchanged := s.record != nil && s.record.Settings == u.Settings && previous != nil && *previous == v
	record := s.record
	if !unchanged {
		record = &Record{Settings: u.Settings}
		if previous != nil && previous.ClientSecret == v.ClientSecret && s.record.Secret != nil {
			record.Secret = s.record.Secret
		} else {
			if s.sealer == nil {
				return secrets.ErrNoMasterKey
			}
			sealed, err := s.sealer.Seal(secrets.OIDCClientSecret, []byte(v.ClientSecret))
			if err != nil {
				return secrets.ErrUnreadable
			}
			record.Secret = &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: sealed}
		}
		if err := s.repo.SaveOIDC(ctx, actor, record); err != nil {
			return ErrStorage
		}
	}
	s.stateMu.Lock()
	s.record = record
	s.problem = ""
	s.stateMu.Unlock()
	snap := &Snapshot{}
	if p != nil {
		snap = &Snapshot{Provider: p, Issuer: v.Issuer, ProviderName: u.ProviderName}
	}
	s.cur.Store(snap)
	return nil
}
func (s *Service) SetEnabled(ctx context.Context, actor string, enabled bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.env != (Values{}) {
		return ErrReadOnly
	}
	if s.record == nil {
		return ErrInvalid
	}
	if !enabled {
		if !s.record.Settings.Enabled {
			return nil
		}
		if err := s.keepASignInPath(ctx); err != nil {
			return err
		}
		r := *s.record
		r.Settings.Enabled = false
		if err := s.repo.SaveOIDC(ctx, actor, &r); err != nil {
			return ErrStorage
		}
		s.stateMu.Lock()
		s.record = &r
		s.problem = ""
		s.stateMu.Unlock()
		s.cur.Store(&Snapshot{})
		return nil
	}
	u := Update{Settings: s.record.Settings}
	u.Enabled = enabled
	return s.save(ctx, actor, u)
}
func (s *Service) Discard(ctx context.Context, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.record == nil {
		return nil
	}
	if s.env == (Values{}) && s.OIDCWorking() {
		if err := s.keepASignInPath(ctx); err != nil {
			return err
		}
	}
	if err := s.repo.SaveOIDC(ctx, actor, nil); err != nil {
		return ErrStorage
	}
	s.stateMu.Lock()
	s.record = nil
	if s.env == (Values{}) {
		s.problem = ""
	}
	s.stateMu.Unlock()
	if s.env == (Values{}) {
		s.cur.Store(&Snapshot{})
	}
	return nil
}

// DiscoveryMessage translates only known categories; it never formats an
// arbitrary adapter error, even if it wraps a public sentinel.
func DiscoveryMessage(err error) string {
	for _, safe := range []error{oidc.ErrIssuerURL, oidc.ErrDiscoveryDocument, oidc.ErrDiscoveryMetadata} {
		// Only fixed phrases from the provider adapter are exposed.
		if errors.Is(err, safe) {
			return "OIDC discovery failed: " + safe.Error()
		}
	}
	return ErrDiscovery.Error()
}
