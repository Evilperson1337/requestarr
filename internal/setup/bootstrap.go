package setup

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bootstrapTokenBytes = 32
	BootstrapTokenSize  = 43
	SessionMaxAge       = 15 * time.Minute
)

var (
	ErrBootstrapTokenFormat = errors.New("invalid bootstrap token format")
	ErrPeerAddress          = errors.New("invalid direct peer address")
	ErrOrigin               = errors.New("invalid origin")
	ErrBootstrapUnavailable = errors.New("setup bootstrap unavailable")
)

type BootstrapOutcome uint8

const (
	BootstrapSessionCreated BootstrapOutcome = iota + 1
	BootstrapTokenInvalid
	BootstrapRateLimited
	BootstrapSetupCompleted
	BootstrapSetupLocked
	BootstrapUnavailable
)

type BootstrapResult struct {
	Outcome    BootstrapOutcome
	Session    string
	ExpiresAt  time.Time
	RetryAfter time.Duration
}

// BootstrapService owns temporary setup capability policy. It exposes only
// opaque cookie material; raw deployment tokens never cross into persistence.
type BootstrapService struct {
	persistence BootstrapPersistence
	random      io.Reader
	key         Digest
	verifier    Digest
	locked      bool
	now         func() time.Time
	cleanupMu   sync.Mutex
	lastCleanup time.Time
	cleanupFail func()
}

func NewBootstrapService(persistence BootstrapPersistence, token [32]byte, configured bool) *BootstrapService {
	service := &BootstrapService{persistence: persistence, random: rand.Reader, locked: !configured, now: time.Now}
	if configured {
		service.key = keyedDigest(token[:], []byte("requestarr/setup/v1/key"))
		service.verifier = bootstrapVerifier(service.key, token)
	}
	return service
}

// WithRandomSource is intended for deterministic tests and composition-time
// fault injection. Production construction retains crypto/rand.Reader.
func (s *BootstrapService) WithRandomSource(source io.Reader) *BootstrapService {
	if source != nil {
		s.random = source
	}
	return s
}

// WithCleanupFailureHandler installs a secret-free operational callback. The
// concrete persistence error is deliberately not exposed across this boundary.
func (s *BootstrapService) WithCleanupFailureHandler(handler func()) *BootstrapService {
	s.cleanupFail = handler
	return s
}

func (s *BootstrapService) Exchange(ctx context.Context, candidate string, peer netip.Addr) BootstrapResult {
	if s == nil || s.persistence == nil || ctx.Err() != nil || !peer.IsValid() {
		return BootstrapResult{Outcome: BootstrapUnavailable}
	}
	candidateBytes, err := DecodeBootstrapToken(candidate)
	if err != nil {
		return BootstrapResult{Outcome: BootstrapUnavailable}
	}

	peerKey := peerDigest(s.key, peer)
	globalKey := keyedDigest(s.key[:], []byte("requestarr/setup/v1/rate/global"))
	var sessionBytes [32]byte
	request := ExchangeRequest{
		PeerKey:   peerKey,
		GlobalKey: globalKey,
		Locked:    s.locked,
		Verify: func() bool {
			candidateVerifier := bootstrapVerifier(s.key, candidateBytes)
			return subtle.ConstantTimeCompare(candidateVerifier[:], s.verifier[:]) == 1
		},
		Material: func() (ExchangeMaterial, error) {
			var randomMaterial [96]byte
			if _, err := io.ReadFull(s.random, randomMaterial[:]); err != nil {
				return ExchangeMaterial{}, ErrBootstrapUnavailable
			}
			copy(sessionBytes[:], randomMaterial[:32])
			return ExchangeMaterial{
				SessionID:     persistenceID(randomMaterial[32:48]),
				SessionDigest: sessionDigest(s.key, sessionBytes),
				EventID:       persistenceID(randomMaterial[48:64]),
				PeerEventID:   persistenceID(randomMaterial[64:80]),
				GlobalEventID: persistenceID(randomMaterial[80:96]),
			}, nil
		},
	}
	result, err := s.persistence.Exchange(ctx, request)
	if err != nil {
		return BootstrapResult{Outcome: BootstrapUnavailable}
	}
	switch result.Outcome {
	case ExchangeSessionCreated:
		s.cleanup(ctx)
		return BootstrapResult{
			Outcome:   BootstrapSessionCreated,
			Session:   base64.RawURLEncoding.EncodeToString(sessionBytes[:]),
			ExpiresAt: result.ExpiresAt,
		}
	case ExchangeTokenInvalid:
		return BootstrapResult{Outcome: BootstrapTokenInvalid}
	case ExchangeRateLimited:
		return BootstrapResult{Outcome: BootstrapRateLimited, RetryAfter: result.RetryAfter}
	case ExchangeSetupCompleted:
		return BootstrapResult{Outcome: BootstrapSetupCompleted}
	case ExchangeSetupLocked:
		return BootstrapResult{Outcome: BootstrapSetupLocked}
	default:
		return BootstrapResult{Outcome: BootstrapUnavailable}
	}
}

func (s *BootstrapService) cleanup(ctx context.Context) {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	now := s.now()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < time.Minute {
		return
	}
	s.lastCleanup = now
	if _, err := s.persistence.Cleanup(ctx); err != nil && s.cleanupFail != nil {
		s.cleanupFail()
	}
}

func DecodeBootstrapToken(value string) ([32]byte, error) {
	var token [bootstrapTokenBytes]byte
	if len(value) != BootstrapTokenSize {
		return token, ErrBootstrapTokenFormat
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != len(token) || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return token, ErrBootstrapTokenFormat
	}
	copy(token[:], decoded)
	return token, nil
}

func sessionDigest(key Digest, session [32]byte) Digest {
	return keyedDigest(key[:], appendDomain("requestarr/setup/v1/session", session[:]))
}

func peerDigest(key Digest, peer netip.Addr) Digest {
	peer = peer.Unmap()
	return keyedDigest(key[:], appendDomain("requestarr/setup/v1/rate/peer", peer.AsSlice()))
}

func bootstrapVerifier(key Digest, token [32]byte) Digest {
	return keyedDigest(key[:], appendDomain("requestarr/setup/v1/bootstrap-token", token[:]))
}

func keyedDigest(key, data []byte) Digest {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	var digest Digest
	copy(digest[:], mac.Sum(nil))
	return digest
}

func appendDomain(domain string, value []byte) []byte {
	data := make([]byte, 0, len(domain)+1+len(value))
	data = append(data, domain...)
	data = append(data, 0)
	return append(data, value...)
}

func persistenceID(value []byte) PersistenceID {
	var id PersistenceID
	copy(id[:], value)
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// DirectPeerIP derives identity only from RemoteAddr. Callers must not replace
// it with Forwarded, X-Forwarded-For, or X-Real-IP values.
func DirectPeerIP(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return netip.Addr{}, ErrPeerAddress
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, ErrPeerAddress
	}
	return address.Unmap(), nil
}

// NormalizeOrigin returns a canonical origin with an explicit effective port.
func NormalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrOrigin
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrOrigin
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", ErrOrigin
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", ErrOrigin
	}
	return scheme + "://" + net.JoinHostPort(host, port), nil
}
