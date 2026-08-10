package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
)

type Store struct {
	baseDir string
	maxSize int64
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func NewStore(baseDir string, maxSize int64) (*Store, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	return &Store{
		baseDir: baseDir,
		maxSize: maxSize,
		encoder: encoder,
		decoder: decoder,
	}, nil
}

func (s *Store) Store(data []byte, sourceEventID, command, cwd string, exitCode int) (*EvidenceArtifact, error) {
	if int64(len(data)) > s.maxSize {
		return nil, fmt.Errorf("artifact too large: %d > %d", len(data), s.maxSize)
	}

	digest := sha256.Sum256(data)
	digestHex := hex.EncodeToString(digest[:])
	artifactID := uuid.New().String()

	relPath := filepath.Join("sha256", digestHex[:2], digestHex[2:4], digestHex+".zst")
	absPath := filepath.Join(s.baseDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	var compressed bytes.Buffer
	compressedData := s.encoder.EncodeAll(data, nil)
	compressed.Write(compressedData)

	if err := os.WriteFile(absPath, compressed.Bytes(), 0600); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}

	estTokens := estimateTokens(data)

	return &EvidenceArtifact{
		ArtifactID:      artifactID,
		ContentDigest:   digestHex,
		MediaType:       "text/plain",
		Encoding:        "utf-8",
		OriginalBytes:   int64(len(data)),
		CompressedBytes: int64(compressed.Len()),
		EstimatedTokens: estTokens,
		StoragePath:     absPath,
		SourceEventID:   sourceEventID,
		Command:         command,
		Cwd:             cwd,
		ExitCode:        exitCode,
		CreatedAt:       time.Now(),
		RetentionClass:  RetentionSession,
	}, nil
}

func (s *Store) Retrieve(artifactID string) ([]byte, error) {
	path := filepath.Join(s.baseDir, artifactID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}

	decompressed, err := s.decoder.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	return decompressed, nil
}

func (s *Store) RetrieveByDigest(digest string) ([]byte, error) {
	relPath := filepath.Join("sha256", digest[:2], digest[2:4], digest+".zst")
	absPath := filepath.Join(s.baseDir, relPath)
	return s.RetrieveFromPath(absPath)
}

func (s *Store) RetrieveFromPath(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return s.decoder.DecodeAll(data, nil)
}

func (s *Store) Verify(artifact *EvidenceArtifact, data []byte) bool {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == artifact.ContentDigest
}

func (s *Store) ReadRange(artifact *EvidenceArtifact, start, end int) ([]byte, error) {
	full, err := s.RetrieveByDigest(artifact.ContentDigest)
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(full, []byte("\n"))
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= len(lines) {
		return nil, io.ErrUnexpectedEOF
	}
	if start > end {
		// ponytail: clamp inverted ranges instead of panicking in the slice
		start = end
	}
	return bytes.Join(lines[start:end], []byte("\n")), nil
}

// DeleteOlderThan removes artifact files older than age. This is the
// file-only pass used when no metadata store is available; the gc command
// uses RemoveDigest + its own metadata bookkeeping for DB consistency.
func (s *Store) DeleteOlderThan(age time.Duration) error {
	return filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if time.Since(info.ModTime()) > age {
			os.Remove(path)
		}
		return nil
	})
}

// RemoveDigest removes the file for a digest, tolerating a missing file
// (already collected or never written).
func (s *Store) RemoveDigest(digest string) error {
	path := filepath.Join(s.baseDir, "sha256", digest[:2], digest[2:4], digest+".zst")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// BaseDir exposes the artifact directory for maintenance sweeps.
func (s *Store) BaseDir() string {
	return s.baseDir
}

// OrphanDigests returns digests of files on disk that no metadata row
// references, so gc can sweep leftovers from crashes or manual deletion.
func (s *Store) OrphanDigests(known map[string]bool) []string {
	var orphans []string
	filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		digest := strings.TrimSuffix(filepath.Base(path), ".zst")
		if !known[digest] {
			orphans = append(orphans, path)
		}
		return nil
	})
	return orphans
}

func estimateTokens(data []byte) int {
	return len(data) / 4
}
