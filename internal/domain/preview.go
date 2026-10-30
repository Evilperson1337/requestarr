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
}

type PreviewRequest struct {
	ID         string        `json:"id"`
	Status     RequestStatus `json:"status"`
	Subscribed bool          `json:"subscribed"`
}

type LibraryLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func ValidUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil
}
