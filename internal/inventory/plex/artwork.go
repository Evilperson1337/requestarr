package plex

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"

	"github.com/evilperson1337/requestarr/internal/inventory"
)

// thumbPattern is the only shape of cover reference this adapter keeps or fetches,
// so a reference can never aim a request anywhere but one Plex item's cover.
var thumbPattern = regexp.MustCompile(`^/library/metadata/[0-9]{1,12}/thumb(/[0-9]{1,12})?$`)

const maxArtwork = 4 << 20

var _ inventory.ArtworkSource = (*Client)(nil)

// Artwork fetches a cover through Plex's own resizer, so a poster is a few
// kilobytes rather than the original scan. The token travels only in a header.
func (c *Client) Artwork(ctx context.Context, ref string) (inventory.Artwork, error) {
	if !thumbPattern.MatchString(ref) {
		return inventory.Artwork{}, fmt.Errorf("invalid Plex artwork reference")
	}
	q := url.Values{"url": {ref}, "width": {"360"}, "height": {"540"}, "minSize": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/photo/:/transcode?"+q.Encode(), nil)
	if err != nil {
		return inventory.Artwork{}, fmt.Errorf("create Plex artwork request")
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return inventory.Artwork{}, fmt.Errorf("Plex artwork request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return inventory.Artwork{}, fmt.Errorf("Plex artwork returned HTTP %d", resp.StatusCode)
	}
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (mt != "image/jpeg" && mt != "image/png" && mt != "image/webp") {
		return inventory.Artwork{}, fmt.Errorf("Plex artwork is not an image")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtwork+1))
	if err != nil || len(data) > maxArtwork {
		return inventory.Artwork{}, fmt.Errorf("Plex artwork unreadable or too large")
	}
	return inventory.Artwork{ContentType: mt, Data: data}, nil
}
