package config

import (
	"path/filepath"
	"testing"
)

func TestDeployConfigsLoad(t *testing.T) {
	files, err := filepath.Glob("../../deploy/compose/config/*.yaml")
	if err != nil || len(files) < 2 {
		t.Fatalf("expected deploy configs, got %v %v", files, err)
	}
	for _, f := range files {
		c, err := Load(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if err := c.Finalize(); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}
