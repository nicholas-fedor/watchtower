package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsableDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	file := filepath.Join(dir, "file")

	err := os.WriteFile(file, []byte("x"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "existing directory", dir: dir, want: true},
		{name: "empty path", dir: "", want: false},
		{name: "missing directory", dir: filepath.Join(dir, "missing"), want: false},
		{name: "path is a file", dir: file, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := UsableDir(tt.dir); got != tt.want {
				t.Errorf("UsableDir(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}

	// The probe must not leave files behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("directory holds %d entries after probing, want 1", len(entries))
	}
}

func TestWriteFileAtomic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	for _, content := range []string{"first", "second, replacing the first"} {
		err := WriteFileAtomic(path, []byte(content))
		if err != nil {
			t.Fatalf("WriteFileAtomic() error = %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != content {
			t.Errorf("file holds %q, want %q", got, content)
		}
	}

	// No temporary files remain beside the result.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want 1", len(entries))
	}
}

func TestWriteFileAtomic_MissingDirectory(t *testing.T) {
	t.Parallel()

	err := WriteFileAtomic(filepath.Join(t.TempDir(), "missing", "state.json"), []byte("x"))
	if err == nil {
		t.Error("WriteFileAtomic() into a missing directory returned no error")
	}
}
