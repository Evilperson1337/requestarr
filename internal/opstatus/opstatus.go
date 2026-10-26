// Package opstatus summarizes provider and library Source health for
// administrators from local state only: it never calls an external service.
package opstatus

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/evilperson1337/requestarr/internal/jobs"
)

const (
	runsPerSource = 10
	maxErrors     = 10
)

// Provider is an integration without a sync job; only its configuration is known.
type Provider struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	// Note is a short fixed status such as "stored secret unreadable"; empty when there is nothing to add.
	Note string `json:"note,omitempty"`
}

// SourceRef names a library Source kind. ID is the job-run source id and is
// empty when the Source is not configured.
type SourceRef struct{ Slug, Name, ID string }

type SourceStatus struct {
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Configured    bool       `json:"configured"`
	LastRun       *jobs.Run  `json:"lastRun"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	LastError     string     `json:"lastError"`
}

type ErrorEntry struct {
	Source string   `json:"source"`
	Run    jobs.Run `json:"run"`
}

type Overview struct {
	Providers    []Provider     `json:"providers"`
	Sources      []SourceStatus `json:"sources"`
	RecentErrors []ErrorEntry   `json:"recentErrors"`
}

type RunSource interface {
	RecentRunsBySource(ctx context.Context, perSource int) ([]jobs.SourceRuns, error)
}

type Service struct {
	providers []Provider
	sources   []SourceRef
	runs      RunSource
}

func New(providers []Provider, sources []SourceRef, runs RunSource) *Service {
	return &Service{providers: providers, sources: sources, runs: runs}
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	groups, err := s.runs.RecentRunsBySource(ctx, runsPerSource)
	if err != nil {
		return Overview{}, fmt.Errorf("load recent runs: %w", err)
	}
	byID := make(map[string][]jobs.Run, len(groups))
	for _, group := range groups {
		byID[group.SourceID] = group.Runs
	}
	out := Overview{Providers: append([]Provider{}, s.providers...), Sources: []SourceStatus{}, RecentErrors: []ErrorEntry{}}
	for _, ref := range s.sources {
		status := SourceStatus{Slug: ref.Slug, Name: ref.Name, Configured: ref.ID != ""}
		if status.Configured {
			runs := byID[ref.ID] // newest first
			if len(runs) > 0 {
				latest := runs[0]
				status.LastRun, status.LastError = &latest, latest.Error
			}
			for _, r := range runs {
				if r.Status == jobs.StatusSucceeded && r.FinishedAt != nil && status.LastSuccessAt == nil {
					finished := *r.FinishedAt
					status.LastSuccessAt = &finished
				}
				if r.Status == jobs.StatusFailed {
					out.RecentErrors = append(out.RecentErrors, ErrorEntry{Source: ref.Name, Run: r})
				}
			}
		}
		out.Sources = append(out.Sources, status)
	}
	sort.SliceStable(out.RecentErrors, func(i, j int) bool { return out.RecentErrors[i].Run.StartedAt.After(out.RecentErrors[j].Run.StartedAt) })
	if len(out.RecentErrors) > maxErrors {
		out.RecentErrors = out.RecentErrors[:maxErrors]
	}
	return out, nil
}
