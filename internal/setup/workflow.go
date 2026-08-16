package setup

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.example.com/evilperson1337/Requestarr/internal/strictjson"
)

const (
	GeneralInstanceNameMax         = 100
	GeneralInstanceNameBytesMax    = 400
	GeneralPublicURLMax            = 2048
	GeneralPublicURLBytesMax       = 8192
	GeneralTimezoneMax             = 128
	GeneralTimezoneBytesMax        = 512
	AuthenticationIssuerMax        = 2048
	AuthenticationIssuerBytesMax   = 8192
	AuthenticationClientIDMax      = 256
	AuthenticationClientIDBytesMax = 1024
	AuthenticationScopeMax         = 64
	AuthenticationScopeCountMax    = 32
	AuthenticationGroupsClaimMax   = 128
	AuthenticationGroupMax         = 256
	AuthenticationMappingCountMax  = 64
	AuthenticationClientSecretMax  = 8192
)

type General struct {
	InstanceName string `json:"instance_name"`
	PublicURL    string `json:"public_url"`
	Timezone     string `json:"timezone"`
}

// NormalizeGeneral validates and canonicalizes the non-secret General step.
func NormalizeGeneral(value General) (General, error) {
	value.InstanceName = strings.TrimSpace(value.InstanceName)
	value.PublicURL = strings.TrimSpace(value.PublicURL)
	value.Timezone = strings.TrimSpace(value.Timezone)
	if !validGeneralString(value.InstanceName, GeneralInstanceNameMax, GeneralInstanceNameBytesMax) ||
		!validGeneralString(value.PublicURL, GeneralPublicURLMax, GeneralPublicURLBytesMax) ||
		!validGeneralString(value.Timezone, GeneralTimezoneMax, GeneralTimezoneBytesMax) {
		return General{}, ErrGeneralInvalid
	}
	parsed, err := url.Parse(value.PublicURL)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return General{}, ErrGeneralInvalid
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	value.PublicURL = parsed.String()
	if value.Timezone == "Local" {
		return General{}, ErrGeneralInvalid
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return General{}, ErrGeneralInvalid
	}
	return value, nil
}

func validGeneralString(value string, codePoints, bytes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= codePoints && len(value) <= bytes
}

type WorkflowState struct {
	Current                Step
	General                *General
	Authentication         *Authentication
	ClientSecretConfigured bool
}

type WorkflowOutcome uint8

const (
	WorkflowAvailable WorkflowOutcome = iota + 1
	WorkflowSessionRequired
	WorkflowCompleted
	WorkflowInvalid
	WorkflowLocked
	WorkflowUnavailable
)

type WorkflowResult struct {
	Outcome WorkflowOutcome
	State   WorkflowState
}

var (
	ErrGeneralInvalid        = errors.New("setup general settings invalid")
	ErrAuthenticationInvalid = errors.New("setup authentication settings invalid")
)

// Authentication is the non-secret, locally validated OIDC setup draft.
type Authentication struct {
	Issuer      string             `json:"issuer"`
	ClientID    string             `json:"client_id"`
	ClientType  ClientType         `json:"client_type"`
	Scopes      []string           `json:"scopes"`
	GroupsClaim string             `json:"groups_claim"`
	DefaultRole string             `json:"default_role"`
	GroupRoles  []GroupRoleMapping `json:"group_role_mappings"`
}

type ClientType string

const (
	ClientTypePublic       ClientType = "public"
	ClientTypeConfidential ClientType = "confidential"
)

func (t ClientType) Valid() bool { return t == ClientTypePublic || t == ClientTypeConfidential }

type GroupRoleMapping struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

type SecretOperationKind uint8

const (
	SecretPreserve SecretOperationKind = iota + 1
	SecretReplace
	SecretClear
)

// SecretOperation deliberately keeps plaintext private and is never embedded
// in workflow state or persistence results.
type SecretOperation struct {
	kind  SecretOperationKind
	value []byte
}

// String and GoString make accidental fmt and structured-log rendering safe.
// They intentionally reveal neither operation kind nor the byte representation.
func (SecretOperation) String() string   { return "[REDACTED]" }
func (SecretOperation) GoString() string { return "[REDACTED]" }
func (o SecretOperation) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[REDACTED]")
}

func PreserveSecret() SecretOperation { return SecretOperation{kind: SecretPreserve} }
func ClearSecret() SecretOperation    { return SecretOperation{kind: SecretClear} }

func ReplaceSecret(value string) (SecretOperation, error) {
	if value == "" || len(value) > AuthenticationClientSecretMax || !utf8.ValidString(value) || hasControl(value) {
		return SecretOperation{}, ErrAuthenticationInvalid
	}
	return SecretOperation{kind: SecretReplace, value: []byte(value)}, nil
}

func (o SecretOperation) Kind() SecretOperationKind { return o.kind }

// PlaintextEqual performs the only supported comparison without exporting the
// submitted secret value.
func (o SecretOperation) PlaintextEqual(value []byte) bool {
	return o.kind == SecretReplace && subtle.ConstantTimeCompare(o.value, value) == 1
}

// EncryptValue calls the provided closure without exposing plaintext through a
// return value. Callers use it only to create an envelope.
func (o SecretOperation) EncryptValue(fn func([]byte) error) error {
	if o.kind != SecretReplace || fn == nil {
		return ErrAuthenticationInvalid
	}
	return fn(o.value)
}

func NormalizeAuthentication(value Authentication) (Authentication, error) {
	value.Issuer = strings.TrimSpace(value.Issuer)
	value.ClientID = strings.TrimSpace(value.ClientID)
	value.GroupsClaim = strings.TrimSpace(value.GroupsClaim)
	value.DefaultRole = strings.TrimSpace(value.DefaultRole)
	if !validAuthenticationString(value.Issuer, AuthenticationIssuerMax, AuthenticationIssuerBytesMax) ||
		!validAuthenticationString(value.ClientID, AuthenticationClientIDMax, AuthenticationClientIDBytesMax) ||
		!validAuthenticationString(value.GroupsClaim, AuthenticationGroupsClaimMax, AuthenticationGroupsClaimMax*4) ||
		!value.ClientType.Valid() || !validRole(value.DefaultRole) || len(value.Scopes) == 0 || len(value.Scopes) > AuthenticationScopeCountMax ||
		len(value.GroupRoles) > AuthenticationMappingCountMax {
		return Authentication{}, ErrAuthenticationInvalid
	}
	issuer, err := url.Parse(value.Issuer)
	if err != nil || !issuer.IsAbs() || issuer.Opaque != "" || issuer.User != nil || issuer.Host == "" ||
		(issuer.Scheme != "http" && issuer.Scheme != "https") || issuer.RawQuery != "" || issuer.Fragment != "" {
		return Authentication{}, ErrAuthenticationInvalid
	}
	if ambiguousIssuerPath(issuer) {
		return Authentication{}, ErrAuthenticationInvalid
	}
	issuer.Scheme = strings.ToLower(issuer.Scheme)
	issuer.Host = strings.ToLower(issuer.Host)
	if issuer.Path == "/" {
		issuer.Path = ""
		issuer.RawPath = ""
	} else {
		issuer.Path = strings.TrimSuffix(issuer.Path, "/")
		if issuer.RawPath != "" {
			issuer.RawPath = strings.TrimSuffix(issuer.RawPath, "/")
		}
	}
	value.Issuer = issuer.String()
	seenScopes := make(map[string]struct{}, len(value.Scopes))
	for i := range value.Scopes {
		value.Scopes[i] = strings.TrimSpace(value.Scopes[i])
		if !validScope(value.Scopes[i]) {
			return Authentication{}, ErrAuthenticationInvalid
		}
		if _, exists := seenScopes[value.Scopes[i]]; exists {
			return Authentication{}, ErrAuthenticationInvalid
		}
		seenScopes[value.Scopes[i]] = struct{}{}
	}
	if _, ok := seenScopes["openid"]; !ok {
		return Authentication{}, ErrAuthenticationInvalid
	}
	seenGroups := make(map[string]struct{}, len(value.GroupRoles))
	for i := range value.GroupRoles {
		value.GroupRoles[i].Group = strings.TrimSpace(value.GroupRoles[i].Group)
		value.GroupRoles[i].Role = strings.TrimSpace(value.GroupRoles[i].Role)
		if !validAuthenticationString(value.GroupRoles[i].Group, AuthenticationGroupMax, AuthenticationGroupMax*4) ||
			!validRole(value.GroupRoles[i].Role) {
			return Authentication{}, ErrAuthenticationInvalid
		}
		if _, exists := seenGroups[value.GroupRoles[i].Group]; exists {
			return Authentication{}, ErrAuthenticationInvalid
		}
		seenGroups[value.GroupRoles[i].Group] = struct{}{}
	}
	return value, nil
}

func ambiguousIssuerPath(issuer *url.URL) bool {
	escaped := issuer.EscapedPath()
	for _, segment := range strings.Split(escaped, "/") {
		lower := strings.ToLower(segment)
		if lower == "." || lower == ".." || lower == "%2e" || lower == "%2e%2e" ||
			strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return true
		}
	}
	return false
}

func validAuthenticationString(value string, runes, bytes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= runes && len(value) <= bytes && !hasControl(value)
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func validScope(value string) bool {
	if value == "" || len(value) > AuthenticationScopeMax {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r >= 0x7f || r == '"' || r == '\\' {
			return false
		}
	}
	return true
}

func validRole(value string) bool {
	return value == "viewer" || value == "requester" || value == "admin"
}

// WorkflowPersistence accepts only keyed session digests. Raw cookie material
// cannot cross this boundary.
type WorkflowPersistence interface {
	ReadWorkflow(context.Context, Digest) (WorkflowResult, error)
	SaveGeneral(context.Context, Digest, General) (WorkflowResult, error)
	SaveAuthentication(context.Context, Digest, Authentication, SecretOperation) (WorkflowResult, error)
}

func (s *WorkflowService) PutAuthentication(ctx context.Context, rawSession string, value Authentication, secret SecretOperation) WorkflowResult {
	normalized, err := NormalizeAuthentication(value)
	if err != nil || (secret.Kind() != SecretPreserve && secret.Kind() != SecretReplace && secret.Kind() != SecretClear) {
		return WorkflowResult{Outcome: WorkflowInvalid}
	}
	digest, ok := s.digestSession(rawSession)
	if !ok {
		return WorkflowResult{Outcome: WorkflowSessionRequired}
	}
	if ctx.Err() != nil || s.persistence == nil {
		return WorkflowResult{Outcome: WorkflowUnavailable}
	}
	result, err := s.persistence.SaveAuthentication(ctx, digest, normalized, secret)
	if err != nil {
		return WorkflowResult{Outcome: WorkflowUnavailable}
	}
	return result
}

type WorkflowService struct {
	persistence WorkflowPersistence
	key         Digest
	configured  bool
}

func NewWorkflowService(persistence WorkflowPersistence, token [32]byte, configured bool) *WorkflowService {
	service := &WorkflowService{persistence: persistence, configured: configured}
	if configured {
		service.key = keyedDigest(token[:], []byte("requestarr/setup/v1/key"))
	}
	return service
}

func (s *WorkflowService) State(ctx context.Context, rawSession string) WorkflowResult {
	digest, ok := s.digestSession(rawSession)
	if !ok || ctx.Err() != nil || s.persistence == nil {
		return WorkflowResult{Outcome: outcomeForUnavailableSession(ctx, ok, s)}
	}
	result, err := s.persistence.ReadWorkflow(ctx, digest)
	if err != nil {
		return WorkflowResult{Outcome: WorkflowUnavailable}
	}
	return result
}

func (s *WorkflowService) PutGeneral(ctx context.Context, rawSession string, value General) WorkflowResult {
	normalized, err := NormalizeGeneral(value)
	if err != nil {
		return WorkflowResult{Outcome: WorkflowInvalid}
	}
	digest, ok := s.digestSession(rawSession)
	if !ok {
		return WorkflowResult{Outcome: WorkflowSessionRequired}
	}
	if ctx.Err() != nil || s.persistence == nil {
		return WorkflowResult{Outcome: WorkflowUnavailable}
	}
	result, err := s.persistence.SaveGeneral(ctx, digest, normalized)
	if err != nil {
		return WorkflowResult{Outcome: WorkflowUnavailable}
	}
	return result
}

func outcomeForUnavailableSession(ctx context.Context, valid bool, service *WorkflowService) WorkflowOutcome {
	if !valid || service == nil || !service.configured {
		return WorkflowSessionRequired
	}
	return WorkflowUnavailable
}

func (s *WorkflowService) digestSession(raw string) (Digest, bool) {
	if s == nil || !s.configured || len(raw) != BootstrapTokenSize {
		return Digest{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != bootstrapTokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return Digest{}, false
	}
	var session [bootstrapTokenBytes]byte
	copy(session[:], decoded)
	return sessionDigest(s.key, session), true
}

type WorkflowDraft struct {
	Version        int             `json:"version"`
	General        *General        `json:"general,omitempty"`
	Authentication *Authentication `json:"authentication,omitempty"`
}

func EncodeWorkflowDraft(draft WorkflowDraft) ([]byte, error) {
	draft.Version = 2
	if draft.General == nil && draft.Authentication == nil {
		return nil, ErrStateInvalid
	}
	return json.Marshal(draft)
}

func DecodeWorkflowDraft(data []byte) (WorkflowDraft, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("{}")) {
		return WorkflowDraft{Version: 2}, nil
	}
	if err := strictjson.RejectDuplicateKeys(data); err != nil {
		return WorkflowDraft{}, ErrStateInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var draft WorkflowDraft
	if err := decoder.Decode(&draft); err != nil || (draft.Version != 1 && draft.Version != 2) ||
		(draft.Version == 1 && (draft.General == nil || draft.Authentication != nil)) ||
		(draft.Version == 2 && draft.General == nil && draft.Authentication == nil) {
		return WorkflowDraft{}, ErrStateInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return WorkflowDraft{}, ErrStateInvalid
	}
	if draft.General != nil {
		normalized, err := NormalizeGeneral(*draft.General)
		if err != nil || normalized != *draft.General {
			return WorkflowDraft{}, ErrStateInvalid
		}
	}
	if draft.Authentication != nil {
		normalized, err := NormalizeAuthentication(*draft.Authentication)
		if err != nil || !AuthenticationEqual(normalized, *draft.Authentication) {
			return WorkflowDraft{}, ErrStateInvalid
		}
	}
	return draft, nil
}

func AuthenticationEqual(a, b Authentication) bool {
	return a.Issuer == b.Issuer && a.ClientID == b.ClientID && a.ClientType == b.ClientType && a.GroupsClaim == b.GroupsClaim &&
		a.DefaultRole == b.DefaultRole && slices.Equal(a.Scopes, b.Scopes) && slices.Equal(a.GroupRoles, b.GroupRoles)
}
