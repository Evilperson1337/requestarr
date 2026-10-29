package devlogs_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/setup"
)

func TestBoundedConcurrentSnapshot(t *testing.T) {
	r := &devlogs.Recorder{}
	if got := r.Snapshot(); got.Entries == nil || got.Capacity != 200 || len(got.Entries) != 0 {
		t.Fatalf("empty snapshot = %+v", got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Record(setup.TargetTMDB, devlogs.StageRequest, devlogs.CodeHTTPStatus, 401, time.Millisecond)
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Entries) != 200 || s.Entries[0].Seq != 601 || s.Entries[199].Seq != 800 {
		t.Fatalf("ring bounds = %+v", s)
	}
	for i, e := range s.Entries {
		if e.Seq != uint64(601+i) || e.Time.IsZero() || e.DurationMs != 1 || e.HTTPStatus != 401 || e.Outcome != "failure" {
			t.Fatalf("entry = %+v", e)
		}
	}
	s.Entries[0].Seq = 0
	if r.Snapshot().Entries[0].Seq != 601 {
		t.Fatal("snapshot aliases recorder storage")
	}
}

func TestRecorderRejectsUntrustedStrings(t *testing.T) {
	r := &devlogs.Recorder{}
	const canary = "https://username:credential@example.test/?token=secret"
	r.Record(setup.Target(canary), devlogs.StageRequest, devlogs.CodeConnected, 200, 0)
	r.Record(setup.TargetTMDB, devlogs.Stage(canary), devlogs.CodeConnected, 200, 0)
	r.Record(setup.TargetTMDB, devlogs.StageRequest, devlogs.Code(canary), 200, 0)
	if len(r.Snapshot().Entries) != 0 {
		t.Fatal("accepted untrusted enum")
	}
	r.Record(setup.TargetTMDB, devlogs.StageRequest, devlogs.CodeConnected, 123456789, -time.Second)
	data, err := json.Marshal(r.Snapshot())
	if err != nil || strings.Contains(string(data), canary) || strings.Contains(string(data), "httpStatus") || r.Snapshot().Entries[0].DurationMs != 0 {
		t.Fatalf("unsafe snapshot: %s, %v", data, err)
	}
	var disabled *devlogs.Recorder
	disabled.Record(setup.TargetTMDB, devlogs.StageCheck, devlogs.CodeConnected, 0, 0)
}
