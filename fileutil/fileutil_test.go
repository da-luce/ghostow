package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathExists(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	if !PathExists(tmpFile) {
		t.Errorf("expected PathExists to return true for existing file")
	}
	if PathExists(tmpFile + "_nope") {
		t.Errorf("expected PathExists to return false for non-existing file")
	}
}

func TestIsRegularFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(tmpFile, []byte("hi"), 0644)
	if !IsRegularFile(tmpFile) {
		t.Errorf("expected IsRegularFile true for a regular file")
	}
}

func TestIsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, []byte("hi"), 0644)
	os.Symlink(target, link)
	if !IsSymlink(link) {
		t.Errorf("expected IsSymlink true for symlink")
	}
	if IsSymlink(target) {
		t.Errorf("expected IsSymlink false for regular file")
	}
}

func TestIsDir(t *testing.T) {
	dir := t.TempDir()
	if !IsDir(dir) {
		t.Errorf("expected IsDir true for directory")
	}
}

func TestIsChildPath(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	os.Mkdir(child, 0755)

	isChild, err := IsChildPath(child, parent)
	if err != nil || !isChild {
		t.Errorf("expected %q to be child of %q", child, parent)
	}

	isChild, err = IsChildPath(parent, child)
	if err != nil || isChild {
		t.Errorf("expected %q not to be child of %q", parent, child)
	}
}

func TestCompareFileHashes(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "f1.txt")
	f2 := filepath.Join(dir, "f2.txt")
	f3 := filepath.Join(dir, "f3.txt")
	os.WriteFile(f1, []byte("data"), 0644)
	os.WriteFile(f2, []byte("data"), 0644)
	os.WriteFile(f3, []byte("diff"), 0644)

	diff, err := CompareFileHashes(f1, f2)
	if err != nil {
		t.Fatal(err)
	}
	if diff {
		t.Errorf("expected files %q and %q to have same hash", f1, f2)
	}

	diff, err = CompareFileHashes(f1, f3)
	if err != nil {
		t.Fatal(err)
	}
	if !diff {
		t.Errorf("expected files %q and %q to have different hash", f1, f3)
	}
}

func TestMatchesPatterns(t *testing.T) {
	patterns := []string{"*.txt", "file?.md"}

	matched, err := MatchesPatterns("notes.txt", patterns)
	if err != nil || !matched {
		t.Errorf("expected 'notes.txt' to match patterns")
	}

	matched, err = MatchesPatterns("file1.md", patterns)
	if err != nil || !matched {
		t.Errorf("expected 'file1.md' to match patterns")
	}

	matched, err = MatchesPatterns("file12.md", patterns)
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Errorf("expected 'file12.md' not to match patterns")
	}
}
