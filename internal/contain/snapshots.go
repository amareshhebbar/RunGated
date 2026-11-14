package contain

import (
	"context"
	"os"
	"path/filepath"
)

type FileSnapshots struct {
	Dir string
}

func (f *FileSnapshots) Put(_ context.Context, key string, data []byte) (string, error) {
	p := filepath.Join(f.Dir, filepath.FromSlash(filepath.Clean("/" + key))[1:])
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", err
	}
	return "file://" + p, nil
}
