package musicbrainz

import (
	"context"
	"testing"
	"time"
)

// MusicBrainz allows about one request per second; calls are spaced, not bursted.
func TestSpacerSeparatesCallsAndHonorsCancellation(t *testing.T) {
	s := &spacer{interval: 60 * time.Millisecond}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 110*time.Millisecond {
		t.Fatalf("three calls took %v; they must be at least two intervals apart", elapsed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.wait(ctx); err == nil {
		t.Fatal("a cancelled wait must return its error")
	}
}
