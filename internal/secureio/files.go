// Package secureio provides bounded input and private, atomic file output.
package secureio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Read(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("input exceeds %d bytes", max)
	}
	return b, nil
}
func ReadFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f, max)
}

// CheckOutput catches common output mistakes before a potentially billed call.
// Write still performs an atomic exclusive commit to handle races.
func CheckOutput(path string, overwrite bool) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !overwrite {
			return fmt.Errorf("%s already exists; use --force to replace it", path)
		}
		if info.IsDir() {
			return fmt.Errorf("output path is a directory: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return fmt.Errorf("output parent is not a directory")
	}
	return nil
}

// Write commits a same-directory temporary file. Link provides exclusive
// creation without a check-then-rename race when overwriting is disallowed.
func Write(path string, overwrite bool, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".masky-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(tmp, path)
	}
	if err = os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; use --force to replace it", path)
		}
		return err
	}
	return nil
}
