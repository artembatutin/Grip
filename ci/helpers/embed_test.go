package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractContentAddressedAndIdempotent(t *testing.T) {
	root := t.TempDir()
	dir, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != Identity() {
		t.Fatalf("cache directory = %q, want identity %q", dir, Identity())
	}
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
	}
	again, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if again != dir {
		t.Fatalf("second extraction = %q, want %q", again, dir)
	}
}

func TestDirectoryRejectsIncompleteOverride(t *testing.T) {
	t.Setenv("GRIP_HELPER_DIR", t.TempDir())
	if _, err := Directory(); err == nil {
		t.Fatal("Directory accepted an incomplete GRIP_HELPER_DIR")
	}
}
