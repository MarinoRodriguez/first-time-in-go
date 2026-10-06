package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type LocalStorage struct {
	baseDir string
}

func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &LocalStorage{baseDir: baseDir}, nil
}

// SaveStream streams data from reader to a new file with UUID name.
// Returns the allocated UUID, the full file path and the total bytes written.
func (s *LocalStorage) SaveStream(r io.Reader) (string, string, int64, error) {
	id := uuid.New().String()
	filePath := filepath.Join(s.baseDir, id)

	f, err := os.Create(filePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create storage file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, r)
	if err != nil {
		// Clean up partial file on failure
		_ = os.Remove(filePath)
		return "", "", 0, fmt.Errorf("failed to write stream to storage: %w", err)
	}

	return id, filePath, written, nil
}

// OpenFile opens the stored file for reading/streaming to the HTTP client.
func (s *LocalStorage) OpenFile(filePath string) (*os.File, error) {
	return os.Open(filePath)
}

// DeleteFile deletes the physical file from disk.
func (s *LocalStorage) DeleteFile(filePath string) error {
	if filePath == "" {
		return nil
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}
	return nil
}
