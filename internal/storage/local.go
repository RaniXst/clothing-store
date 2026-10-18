package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxFileSize = 5 << 20

type Local struct {
	root string
}

type StagedFile struct {
	Filename string
	Moved    bool
}

func NewLocal(root string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("upload directory cannot be empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve upload directory: %w", err)
	}
	store := &Local{root: absolute}
	for _, directory := range []string{store.root, store.tempDir(), store.trashDir()} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create upload directory: %w", err)
		}
	}
	return store, nil
}

func (s *Local) Prepare(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxFileSize {
		return "", fmt.Errorf("file size must be between 1 byte and %d bytes", MaxFileSize)
	}
	name := uuid.NewString() + ".tmp"
	file, err := os.OpenFile(filepath.Join(s.tempDir(), name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create temporary upload: %w", err)
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(filepath.Join(s.tempDir(), name))
		return "", fmt.Errorf("write temporary upload: %w", err)
	}
	return name, nil
}

func (s *Local) Publish(tempName, filename string) error {
	if !validTempName(tempName) || !validFilename(filename) {
		return fmt.Errorf("invalid generated file name")
	}
	if err := os.Rename(filepath.Join(s.tempDir(), tempName), filepath.Join(s.root, filename)); err != nil {
		return fmt.Errorf("publish uploaded file: %w", err)
	}
	return nil
}

func (s *Local) RemoveTemp(name string) error {
	if !validTempName(name) {
		return fmt.Errorf("invalid temporary file name")
	}
	if err := os.Remove(filepath.Join(s.tempDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Local) Remove(filename string) error {
	if !validFilename(filename) {
		return fmt.Errorf("invalid stored file name")
	}
	if err := os.Remove(filepath.Join(s.root, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Local) Read(filename string) ([]byte, error) {
	if !validFilename(filename) {
		return nil, fmt.Errorf("invalid stored file name")
	}
	path := filepath.Join(s.root, filename)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileSize {
		return nil, fmt.Errorf("stored image is not a regular file within the size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("stored image exceeds size limit")
	}
	return data, nil
}

func (s *Local) StageDelete(filenames []string) ([]StagedFile, error) {
	staged := make([]StagedFile, 0, len(filenames))
	seen := make(map[string]struct{}, len(filenames))
	for _, filename := range filenames {
		if !validFilename(filename) {
			_ = s.Restore(staged)
			return nil, fmt.Errorf("invalid stored file name")
		}
		if _, ok := seen[filename]; ok {
			continue
		}
		seen[filename] = struct{}{}
		entry := StagedFile{Filename: filename}
		if err := os.Rename(filepath.Join(s.root, filename), filepath.Join(s.trashDir(), filename)); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				_ = s.Restore(staged)
				return nil, fmt.Errorf("move image to deletion staging: %w", err)
			}
		} else {
			entry.Moved = true
		}
		staged = append(staged, entry)
	}
	return staged, nil
}

func (s *Local) Restore(staged []StagedFile) error {
	var result error
	for i := len(staged) - 1; i >= 0; i-- {
		entry := staged[i]
		if !entry.Moved {
			continue
		}
		tombstone := filepath.Join(s.trashDir(), entry.Filename)
		destination := filepath.Join(s.root, entry.Filename)
		if _, err := os.Lstat(destination); err == nil {
			result = errors.Join(result, fmt.Errorf("cannot restore %s: destination already exists", entry.Filename))
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
			continue
		}
		if err := os.Rename(tombstone, destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", entry.Filename, err))
		}
	}
	return result
}

func (s *Local) Purge(staged []StagedFile) error {
	var result error
	for _, entry := range staged {
		if !entry.Moved {
			continue
		}
		if err := os.Remove(filepath.Join(s.trashDir(), entry.Filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove staged file %s: %w", entry.Filename, err))
		}
	}
	return result
}

// Recover reconciles staged and orphaned files against filenames still present
// in SQLite. Unreferenced published files and abandoned upload temps older than
// one hour are removed; referenced tombstones are restored.
func (s *Local) Recover(referenced map[string]struct{}) error {
	var result error
	trashEntries, err := os.ReadDir(s.trashDir())
	if err != nil {
		return fmt.Errorf("read deletion staging directory: %w", err)
	}
	for _, entry := range trashEntries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !validFilename(name) {
			continue
		}
		tombstone := filepath.Join(s.trashDir(), name)
		destination := filepath.Join(s.root, name)
		if _, ok := referenced[name]; ok {
			if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, os.Rename(tombstone, destination))
			} else if err != nil {
				result = errors.Join(result, err)
			} else {
				result = errors.Join(result, os.Remove(tombstone))
			}
		} else {
			result = errors.Join(result, os.Remove(tombstone))
		}
	}

	cutoff := time.Now().Add(-time.Hour)
	tempEntries, err := os.ReadDir(s.tempDir())
	if err != nil {
		return errors.Join(result, fmt.Errorf("read upload temporary directory: %w", err))
	}
	for _, entry := range tempEntries {
		if !entry.Type().IsRegular() || !validTempName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			result = errors.Join(result, os.Remove(filepath.Join(s.tempDir(), entry.Name())))
		}
	}

	rootEntries, err := os.ReadDir(s.root)
	if err != nil {
		return errors.Join(result, fmt.Errorf("read upload directory: %w", err))
	}
	for _, entry := range rootEntries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !validFilename(name) {
			continue
		}
		if _, ok := referenced[name]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			result = errors.Join(result, os.Remove(filepath.Join(s.root, name)))
		}
	}
	return result
}

func (s *Local) tempDir() string  { return filepath.Join(s.root, ".tmp") }
func (s *Local) trashDir() string { return filepath.Join(s.root, ".trash") }

func validFilename(name string) bool {
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return false
	}
	ext := filepath.Ext(name)
	if ext != ".jpg" && ext != ".png" {
		return false
	}
	_, err := uuid.Parse(strings.TrimSuffix(name, ext))
	return err == nil
}

func validTempName(name string) bool {
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || filepath.Ext(name) != ".tmp" {
		return false
	}
	_, err := uuid.Parse(strings.TrimSuffix(name, ".tmp"))
	return err == nil
}
