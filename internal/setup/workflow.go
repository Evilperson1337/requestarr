package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	GeneralInstanceNameMax      = 100
	GeneralInstanceNameBytesMax = 400
	GeneralPublicURLMax         = 2048
	GeneralPublicURLBytesMax    = 8192
	GeneralTimezoneMax          = 128
	GeneralTimezoneBytesMax     = 512
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
	Current Step
	General *General
}

type WorkflowOutcome uint8

const (
	WorkflowAvailable WorkflowOutcome = iota + 1
	WorkflowSessionRequired
	WorkflowCompleted
	WorkflowInvalid
	WorkflowUnavailable
)

type WorkflowResult struct {
	Outcome WorkflowOutcome
	State   WorkflowState
}

var ErrGeneralInvalid = errors.New("setup general settings invalid")

// WorkflowPersistence accepts only keyed session digests. Raw cookie material
// cannot cross this boundary.
type WorkflowPersistence interface {
	ReadWorkflow(context.Context, Digest) (WorkflowResult, error)
	SaveGeneral(context.Context, Digest, General) (WorkflowResult, error)
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

type workflowDraft struct {
	Version int      `json:"version"`
	General *General `json:"general,omitempty"`
}

func EncodeWorkflowDraft(general General) ([]byte, error) {
	return json.Marshal(workflowDraft{Version: 1, General: &general})
}

func DecodeWorkflowDraft(data []byte) (*General, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("{}")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var draft workflowDraft
	if err := decoder.Decode(&draft); err != nil || draft.Version != 1 || draft.General == nil {
		return nil, ErrStateInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrStateInvalid
	}
	normalized, err := NormalizeGeneral(*draft.General)
	if err != nil || normalized != *draft.General {
		return nil, ErrStateInvalid
	}
	return &normalized, nil
}
