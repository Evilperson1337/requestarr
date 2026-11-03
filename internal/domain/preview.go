package domain

import (
	"encoding/hex"
	"strings"
)

type MoviePreview struct {
	Availability Availability    `json:"availability"`
	Request      *PreviewRequest `json:"request,omitempty"`
	Libraries    []LibraryLink   `json:"libraries,omitempty"`
	CanRequest   bool            `json:"canRequest"`
	CanSubscribe bool            `json:"canSubscribe"`
	// CanExtend is true when the caller may add to an open Request that covers
	// only part of the item (D027).
	CanExtend bool `json:"canExtend"`
}

type PreviewRequest struct {
	ID         string        `json:"id"`
	Status     RequestStatus `json:"status"`
	Subscribed bool          `json:"subscribed"`
	Selection  Selection     `json:"selection"`
}

type LibraryLink struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

func ValidUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil
}
