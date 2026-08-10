package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

func newArtStore(t *testing.T, maxSize int64) (*artifacts.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := artifacts.NewStore(filepath.Join(dir, "artifacts"), maxSize)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestStoreAtExactMaxSize(t *testing.T) {
	s, _ := newArtStore(t, 100)
	if _, err := s.Store(make([]byte, 100), "e", "cmd", "", 0); err != nil {
		t.Errorf("store at exactly maxSize should succeed, got %v", err)
	}
}

func TestStoreOverMaxSize(t *testing.T) {
	s, _ := newArtStore(t, 100)
	if _, err := s.Store(make([]byte, 101), "e", "cmd", "", 0); err == nil {
		t.Error("store over maxSize should fail")
	}
}

func TestReadRangeEdges(t *testing.T) {
	s, _ := newArtStore(t, 1<<20)
	a, err := s.Store([]byte("l1\nl2\nl3\nl4\n"), "e", "cmd", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		start, end int
		wantErr    bool
	}{
		{"normal", 1, 3, false},
		{"start at len(lines)", 4, 5, false}, // 5 split elements incl. trailing empty
		{"start beyond", 9, 10, true},
		{"end beyond clamped", 1, 99, false},
		{"negative start clamped", -3, 2, false},
		{"start after end clamps", 3, 1, false}, // regression: used to panic
	}
	for _, c := range cases {
		_, err := s.ReadRange(a, c.start, c.end)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: ReadRange(%d,%d) err = %v, wantErr %v", c.name, c.start, c.end, err, c.wantErr)
		}
	}
}

func TestRetrieveByMissingDigest(t *testing.T) {
	s, _ := newArtStore(t, 1<<20)
	_, err := s.RetrieveByDigest("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Error("retrieving a missing digest should error")
	}
}

// Same content twice: same digest, distinct artifact IDs, one physical file.
func TestDeduplication(t *testing.T) {
	s, dir := newArtStore(t, 1<<20)
	data := []byte("identical content for dedup")
	a1, err := s.Store(data, "e1", "cmd", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.Store(data, "e2", "cmd", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if a1.ContentDigest != a2.ContentDigest {
		t.Error("same content should produce same digest")
	}
	if a1.ArtifactID == a2.ArtifactID {
		t.Error("distinct stores should produce distinct artifact IDs")
	}
	files := 0
	filepath.Walk(filepath.Join(dir, "artifacts", "sha256"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
		}
		return nil
	})
	if files != 1 {
		t.Errorf("expected 1 physical file for duplicated content, got %d", files)
	}
}

// GC removes files but not SQLite metadata rows: the known drift.
func TestGCRemovesFilesButNotDBRows(t *testing.T) {
	s, dir := newArtStore(t, 1<<20)
	artifact, err := s.Store([]byte("gc me"), "e", "cmd", "", 0)
	if err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(dir, "costmax.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.InsertArtifact(artifact); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteOlderThan(0); err != nil {
		t.Fatal(err)
	}

	// Files gone?
	if _, err := os.Stat(artifact.StoragePath); !os.IsNotExist(err) {
		t.Error("artifact file should be deleted by gc")
	}
	// DB rows?
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM artifacts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	t.Logf("after gc: %d artifact rows remain in DB (drift)", count)
	if count != 1 {
		t.Errorf("expected 1 stale DB row (drift), got %d", count)
	}
}

func TestVerifyTamperDetection(t *testing.T) {
	s, _ := newArtStore(t, 1<<20)
	a, err := s.Store([]byte("original"), "e", "cmd", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Verify(a, []byte("original")) {
		t.Error("verify should pass on original")
	}
	if s.Verify(a, []byte("tampered!")) {
		t.Error("verify should fail on tampered data")
	}
}
