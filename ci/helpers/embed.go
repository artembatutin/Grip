// Package helpers makes Grip's first-party analysis helpers available to every
// distribution of the Go binary. The source scripts remain beside the CI
// integrations, but are embedded at build time and materialized into a
// content-addressed user cache only when a production run needs them.
package helpers

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed ts.mjs php.php
var assets embed.FS

var names = []string{"ts.mjs", "php.php"}

// Directory returns the directory holding executable helper assets. An explicit
// GRIP_HELPER_DIR is intentionally honored for development and tightly managed
// CI environments; every file is still checked before it is executed.
func Directory() (string, error) {
	if dir := os.Getenv("GRIP_HELPER_DIR"); dir != "" {
		return verify(dir)
	}
	cache, err := os.UserCacheDir()
	if err != nil || cache == "" {
		cache = os.TempDir()
	}
	return Extract(filepath.Join(cache, "grip", "helpers"))
}

// Extract writes the embedded assets beneath root/version only when they do
// not already match. It is exported for install-artifact tests and intentionally
// never uses the consumer repository as a cache location.
func Extract(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("Grip helper cache directory is empty")
	}
	dir := filepath.Join(root, identity())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create Grip helper cache %q: %w", dir, err)
	}
	for _, name := range names {
		body, err := assets.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read embedded helper %q: %w", name, err)
		}
		target := filepath.Join(dir, name)
		if sameFile(target, body) {
			continue
		}
		// A same-directory rename is atomic on supported macOS/Linux filesystems.
		tmp, err := os.CreateTemp(dir, "."+name+"-*")
		if err != nil {
			return "", fmt.Errorf("create helper %q: %w", name, err)
		}
		tmpName := tmp.Name()
		if _, err := tmp.Write(body); err == nil {
			err = tmp.Chmod(0o700)
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tmpName)
			return "", fmt.Errorf("write helper %q: %w", name, err)
		}
		if err := os.Rename(tmpName, target); err != nil {
			_ = os.Remove(tmpName)
			return "", fmt.Errorf("install helper %q: %w", name, err)
		}
	}
	return verify(dir)
}

// Identity is a stable content identity for the exact first-party helper set.
// It becomes part of the cache path, preventing collisions across releases.
func Identity() string { return identity() }

func identity() string {
	h := sha256.New()
	for _, name := range names {
		body, _ := assets.ReadFile(name)
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(body)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func verify(dir string) (string, error) {
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("Grip helper %q is unavailable at %q; unset GRIP_HELPER_DIR to use the embedded helpers: %w", name, path, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("Grip helper %q at %q is a directory", name, path)
		}
	}
	return dir, nil
}

func sameFile(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	return err == nil && string(got) == string(want)
}
