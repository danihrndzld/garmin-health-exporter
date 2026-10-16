// Package atomicfile writes a file so that a killed process never leaves it half written.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFile writes data to a 0600 temporary file next to path and renames it over path.
func WriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(errors.New("could not replace "+filepath.Base(path)), err)
	}
	return nil
}
