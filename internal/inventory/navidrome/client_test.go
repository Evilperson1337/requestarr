package navidrome

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/inventorytest"
)

const (
	testUsername = "navidrome-user"
	testPassword = "navidrome-password"
)

func TestNavidromePassesTheInventorySourceContract(t *testing.T) {
	inventorytest.Run(t, func(t *testing.T, scenario inventorytest.Scenario) inventory.Source {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validTestAuth(r, scenario == inventorytest.Unauthorized) {
				writeResponse(w, http.StatusOK, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Invalid credentials"}}}`)
				return
			}
			if scenario == inventorytest.ServerError {
				http.Error(w, "backend failure", http.StatusInternalServerError)
				return
			}
			if r.URL.Path == "/rest/getMusicFolders.view" {
				writeFixture(t, w, "music_folders.json")
				return
			}
			if r.URL.Path == "/rest/getAlbumList2.view" {
				switch scenario {
				case inventorytest.Empty:
					writeFixture(t, w, "empty_album_list.json")
				case inventorytest.Malformed:
					writeResponse(w, http.StatusOK, "{")
				case inventorytest.Truncated:
					writeResponse(w, http.StatusOK, `{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","albumList2":{}}}`)
				default:
					writeFixture(t, w, "album_list.json")
				}
				return
			}
			if r.URL.Path == "/rest/getAlbum.view" {
				if scenario == inventorytest.MidwayFailure && r.URL.Query().Get("id") == "album-2" {
					http.Error(w, "album unavailable", http.StatusServiceUnavailable)
					return
				}
				switch r.URL.Query().Get("id") {
				case "album-1":
					writeFixture(t, w, "album_1.json")
				case "album-2":
					writeFixture(t, w, "album_2.json")
				default:
					http.NotFound(w, r)
				}
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(ts.Close)
		client, err := NewWithHTTPClient(ts.URL, testUsername, testPassword, ts.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client
	}, inventorytest.Expectations{LibraryItems: 6, Secret: testPassword})
}

func TestSnapshotNormalizesMusicBrainzIdentifiersAndMusicOnlyDescriptor(t *testing.T) {
	source := newTestSource(t)
	snap, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := source.Descriptor()
	if len(d.MediaTypes) != 2 || d.MediaTypes[0] != domain.MediaAlbum || d.MediaTypes[1] != domain.MediaSong {
		t.Fatalf("media types=%v, want album and song only", d.MediaTypes)
	}
	var album, song bool
	for _, item := range snap.Items {
		if item.MediaType == domain.MediaAlbum && item.ExternalIDs["musicbrainz"] == "release-group-1" {
			album = true
			if !strings.Contains(item.ExternalURL, "/app/#/album/") || !strings.HasSuffix(item.ExternalURL, "/show") {
				t.Fatalf("album link = %q", item.ExternalURL)
			}
		}
		if item.MediaType == domain.MediaSong && item.ExternalURL != "" {
			t.Fatalf("a song has no page of its own: %q", item.ExternalURL)
		}
		if item.MediaType == domain.MediaSong && item.ExternalIDs["musicbrainz"] == "recording-1" {
			song = true
			if !strings.HasPrefix(item.ParentSourceItemID, "album:") {
				t.Fatalf("a song must name its album: %+v", item)
			}
		}
	}
	if !album || !song {
		t.Fatalf("snapshot did not preserve normalized MBIDs: %+v", snap.Items)
	}
}

func newTestSource(t *testing.T) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validTestAuth(r, false) {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/rest/getMusicFolders.view":
			writeFixture(t, w, "music_folders.json")
		case "/rest/getAlbumList2.view":
			writeFixture(t, w, "album_list.json")
		case "/rest/getAlbum.view":
			if r.URL.Query().Get("id") == "album-1" {
				writeFixture(t, w, "album_1.json")
			} else {
				writeFixture(t, w, "album_2.json")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	source, err := NewWithHTTPClient(ts.URL, testUsername, testPassword, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func validTestAuth(r *http.Request, reject bool) bool {
	q := r.URL.Query()
	salt := q.Get("s")
	hash := md5.Sum([]byte(testPassword + salt))
	return !reject && q.Get("u") == testUsername && salt != "" && q.Get("t") == hex.EncodeToString(hash[:]) && q.Get("v") == "1.16.1" && q.Get("f") == "json"
}

func writeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	writeResponse(w, http.StatusOK, string(body))
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}
