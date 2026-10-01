package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveServeDir(t *testing.T) {
	t.Run("creates a fresh folder when neither exists", func(t *testing.T) {
		home := t.TempDir()
		got := resolveServeDir(home)
		want := filepath.Join(home, "CableDrop")
		if got != want {
			t.Fatalf("resolveServeDir = %q, want %q", got, want)
		}
		fi, err := os.Stat(want)
		if err != nil || !fi.IsDir() {
			t.Fatalf("folder was not created: %v", err)
		}
	})

	t.Run("keeps the folder when it already exists", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, "CableDrop")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "keep.txt")
		if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := resolveServeDir(home); got != dir {
			t.Fatalf("resolveServeDir = %q, want %q", got, dir)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("existing content disturbed: %v", err)
		}
	})

	t.Run("migrates the pre-rename USBBridge folder once", func(t *testing.T) {
		home := t.TempDir()
		old := filepath.Join(home, "USBBridge")
		if err := os.MkdirAll(old, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(old, "file.txt")
		if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := resolveServeDir(home)
		want := filepath.Join(home, "CableDrop")
		if got != want {
			t.Fatalf("resolveServeDir = %q, want %q", got, want)
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatalf("old folder still there: %v", err)
		}
		if _, err := os.Stat(filepath.Join(want, "file.txt")); err != nil {
			t.Fatalf("content did not move: %v", err)
		}

		// A second pass must be a no-op: the migration is a rename, not a copy.
		if got := resolveServeDir(home); got != want {
			t.Fatalf("second call = %q, want %q", got, want)
		}
	})

	t.Run("leaves both alone when the new folder already exists", func(t *testing.T) {
		home := t.TempDir()
		old := filepath.Join(home, "USBBridge")
		dir := filepath.Join(home, "CableDrop")
		for _, d := range []string{old, dir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := resolveServeDir(home); got != dir {
			t.Fatalf("resolveServeDir = %q, want %q", got, dir)
		}
		if _, err := os.Stat(old); err != nil {
			t.Fatalf("pre-existing old folder was removed: %v", err)
		}
	})
}
