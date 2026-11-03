package snapshottest

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

// absenceInterval mirrors the store's: two complete syncs this far apart count
// as separate absences.
const absenceInterval = 24 * time.Hour

type evidenceKey struct{ catalogID, sourceID, platformKey string }

type absence struct {
	first, last time.Time
}

type memorySource struct {
	enabled bool
	media   map[domain.MediaType]bool
}

// Memory is the in-memory adapter of the snapshot-persistence contract. It
// implements the matching, evidence, absence and availability rules for
// canonical provider ids only; suggested matches and aliases are the store's.
type Memory struct {
	catalog  map[string]*memoryItem
	byTMDB   map[string]string
	requests map[string]*memoryRequest
	sources  map[string]*memorySource
	evidence map[evidenceKey]bool
	absent   map[evidenceKey]absence
	next     int
}

type memoryItem struct {
	media        domain.MediaType
	availability domain.Availability
}

type memoryRequest struct {
	catalogID string
	status    domain.RequestStatus
}

// NewMemory returns an empty adapter.
func NewMemory() *Memory {
	return &Memory{catalog: map[string]*memoryItem{}, byTMDB: map[string]string{}, requests: map[string]*memoryRequest{},
		sources: map[string]*memorySource{}, evidence: map[evidenceKey]bool{}, absent: map[evidenceKey]absence{}}
}

func (m *Memory) id(prefix string) string {
	m.next++
	return prefix + "-" + strconv.Itoa(m.next)
}

func (m *Memory) AddMovie(_ testing.TB, tmdbID string) string {
	id := m.id("catalog")
	m.catalog[id] = &memoryItem{media: domain.MediaMovie, availability: domain.AvailabilityUnknown}
	m.byTMDB[tmdbID] = id
	return id
}

func (m *Memory) OpenRequest(_ testing.TB, catalogID string) string {
	id := m.id("request")
	m.requests[id] = &memoryRequest{catalogID: catalogID, status: domain.RequestRequested}
	return id
}

func (m *Memory) Availability(_ testing.TB, catalogID string) domain.Availability {
	return m.catalog[catalogID].availability
}

func (m *Memory) RequestStatus(_ testing.TB, requestID string) domain.RequestStatus {
	return m.requests[requestID].status
}

func (m *Memory) PersistSnapshot(_ context.Context, d inventory.Descriptor, _ string, snap inventory.Snapshot) error {
	if !snap.Complete || snap.SourceID != d.ID || d.ID == "" {
		return fmt.Errorf("refusing incomplete or unexpected %s snapshot", d.Kind)
	}
	source := &memorySource{enabled: true, media: map[domain.MediaType]bool{}}
	for _, media := range d.MediaTypes {
		source.media[media] = true
	}
	m.sources[d.ID] = source
	for _, item := range snap.Items {
		if item.SourceID != d.ID || item.SourceItemID == "" || !source.media[item.MediaType] {
			return fmt.Errorf("invalid %s inventory item", d.Kind)
		}
	}
	seen := map[evidenceKey]bool{}
	for _, item := range snap.Items {
		catalogID, ok := m.byTMDB[item.ExternalIDs["tmdb"]]
		if !ok || !contains(d.IdentifierKinds, "tmdb") {
			continue
		}
		key := evidenceKey{catalogID, d.ID, item.PlatformKey}
		seen[key] = true
		delete(m.absent, key)
		m.evidence[key] = true
		m.catalog[catalogID].availability = domain.AvailabilityAvailable
		for _, request := range m.requests {
			if request.catalogID == catalogID && (request.status == domain.RequestRequested || request.status == domain.RequestApproved) {
				request.status = domain.RequestAvailable
			}
		}
	}
	// An empty enumeration is ignored for negative evidence unless an operator opts in.
	if len(snap.Items) == 0 {
		return nil
	}
	for key := range m.evidence {
		if key.sourceID != d.ID || seen[key] {
			continue
		}
		streak, started := m.absent[key]
		if !started {
			m.absent[key] = absence{first: snap.ObservedAt, last: snap.ObservedAt}
			continue
		}
		if !snap.ObservedAt.After(streak.last) || snap.ObservedAt.Before(streak.first.Add(absenceInterval)) {
			continue
		}
		delete(m.evidence, key)
		delete(m.absent, key)
		if !m.covered(key.catalogID) {
			m.catalog[key.catalogID].availability = domain.AvailabilityMissing
		}
	}
	return nil
}

// covered reports whether any enabled Source that covers the item's media type
// still has evidence for it (D015).
func (m *Memory) covered(catalogID string) bool {
	media := m.catalog[catalogID].media
	for key := range m.evidence {
		if key.catalogID != catalogID {
			continue
		}
		if source := m.sources[key.sourceID]; source != nil && source.enabled && source.media[media] {
			return true
		}
	}
	return false
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
