// Package audiobookshelf provides a fixture-friendly normalization seam. It
// deliberately has no HTTP client or endpoint knowledge in Phase 5A.
package audiobookshelf

import (
	"fmt"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

// Document is a source-neutral decoded representation supplied by a future
// transport adapter or a deterministic fixture reader.
type Document struct {
	Libraries []Library
	Items     []Item
}

type Library struct{ ID, Name string }

type Item struct {
	LibraryID, ID, Title, URL string
	Details                   domain.AudiobookDetails
}

// Normalize converts a fully decoded, successfully enumerated document into a
// complete generic reader result. Requirements are trusted configuration; the
// document can prove response membership but cannot expand the selected scope.
func Normalize(requirements inventory.CompleteReaderRequirements, completedAt time.Time, document Document) (inventory.CompleteReaderResult, error) {
	if err := validateRequirements(requirements); err != nil {
		return inventory.CompleteReaderResult{}, fmt.Errorf("normalize requirements: %w", err)
	}

	libraries := make(map[string]struct{}, len(document.Libraries))
	for _, library := range document.Libraries {
		id := strings.TrimSpace(library.ID)
		if _, duplicate := libraries[id]; duplicate {
			return inventory.CompleteReaderResult{}, fmt.Errorf("normalize document: duplicate library")
		}
		libraries[id] = struct{}{}
	}

	result := inventory.CompleteReaderResult{
		SourceID:    strings.TrimSpace(requirements.SourceID),
		CompletedAt: completedAt.UTC(),
		Complete:    true,
	}
	for _, libraryID := range requirements.SelectedLibraries {
		libraryID = strings.TrimSpace(libraryID)
		if _, found := libraries[libraryID]; !found {
			return inventory.CompleteReaderResult{}, fmt.Errorf("normalize document: selected library unavailable")
		}
		result.Coverage = append(result.Coverage, inventory.ObservationCoverage{RemoteLibraryID: libraryID, MediaType: domain.MediaAudiobook, Status: inventory.CoverageVerifiedComplete})
	}
	for _, item := range document.Items {
		result.Items = append(result.Items, inventory.CompleteReaderItem{
			RemoteLibraryID: strings.TrimSpace(item.LibraryID),
			SourceItemID:    strings.TrimSpace(item.ID),
			MediaType:       domain.MediaAudiobook,
			Title:           strings.TrimSpace(item.Title),
			ExternalURL:     strings.TrimSpace(item.URL),
		})
	}
	if err := inventory.ValidateCompleteReaderResult(result, requirements); err != nil {
		return inventory.CompleteReaderResult{}, fmt.Errorf("normalize document: %w", err)
	}
	return result, nil
}

func validateRequirements(requirements inventory.CompleteReaderRequirements) error {
	if len(requirements.EnabledMediaTypes) != 1 || requirements.EnabledMediaTypes[0] != domain.MediaAudiobook {
		return fmt.Errorf("audiobook scope is required")
	}
	probe := inventory.CompleteReaderResult{
		SourceID:    strings.TrimSpace(requirements.SourceID),
		CompletedAt: time.Unix(1, 0).UTC(),
		Complete:    true,
	}
	for _, libraryID := range requirements.SelectedLibraries {
		probe.Coverage = append(probe.Coverage, inventory.ObservationCoverage{RemoteLibraryID: strings.TrimSpace(libraryID), MediaType: domain.MediaAudiobook, Status: inventory.CoverageVerifiedComplete})
	}
	return inventory.ValidateCompleteReaderResult(probe, requirements)
}
