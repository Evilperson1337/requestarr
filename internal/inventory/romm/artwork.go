package romm

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"

	"github.com/evilperson1337/requestarr/internal/inventory"
)

// coverPattern is the only shape of cover reference this adapter keeps or
// fetches: one ROM's cover file under RomM's resources directory.
var coverPattern = regexp.MustCompile(`^/assets/romm/resources/roms/[0-9]{1,9}/[0-9]{1,12}/cover/(big|small)\.(png|jpg|jpeg|webp)$`)

const maxArtwork = 4 << 20

var _ inventory.ArtworkSource = (*Client)(nil)

// Artwork fetches a ROM's cover. The token travels only in a header.
func (c *Client) Artwork(ctx context.Context, ref string) (inventory.Artwork, error) {
	if !coverPattern.MatchString(ref) {
		return inventory.Artwork{}, fmt.Errorf("invalid RomM artwork reference")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+ref, nil)
	if err != nil {
		return inventory.Artwork{}, fmt.Errorf("create RomM artwork request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return inventory.Artwork{}, fmt.Errorf("RomM artwork request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return inventory.Artwork{}, fmt.Errorf("RomM artwork returned HTTP %d", resp.StatusCode)
	}
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (mt != "image/jpeg" && mt != "image/png" && mt != "image/webp") {
		return inventory.Artwork{}, fmt.Errorf("RomM artwork is not an image")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtwork+1))
	if err != nil || len(data) > maxArtwork {
		return inventory.Artwork{}, fmt.Errorf("RomM artwork unreadable or too large")
	}
	return inventory.Artwork{ContentType: mt, Data: data}, nil
}
