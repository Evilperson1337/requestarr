package musicbrainz

import (
	"os"
	"testing"
	"time"
)

// Resolver tests make several calls; the spacing itself is tested on its own.
func TestMain(m *testing.M) {
	apiSpacer.interval = time.Millisecond
	os.Exit(m.Run())
}
