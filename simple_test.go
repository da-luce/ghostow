package main

import (
	"ghostow/fileutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test expandPath with ~ symbol for home directory expansion
func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	tests := []struct {
		path     string
		expected string
	}{
		{"~", homeDir},
		{"~/test", homeDir + "/test"},
		{"$HOME/test", homeDir + "/test"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := fileutil.ExpandPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test createSymlink function
func TestCreateSymlink(t *testing.T) {
	source := "./test_source.txt"
	dest := "./test_dest.txt"

	// Create a dummy file for source
	err := os.WriteFile(source, []byte("test content"), 0644)
	if err != nil {
		t.Fatal("Failed to create source file:", err)
	}
	defer os.Remove(source)

	// Test symlink creation
	err = fileutil.CreateSymlink(source, dest, true, true)
	assert.NoError(t, err)

	// Check if symlink exists
	_, err = os.Lstat(dest)
	assert.NoError(t, err)
	isLink := fileutil.IsSymlink(dest)
	assert.NoError(t, err)
	assert.True(t, isLink)

	// Clean up symlink
	defer os.Remove(dest)
}

// Test isSymlink function
func TestIsSymlink(t *testing.T) {
	source := "./test_source.txt"
	dest := "./test_dest.txt"

	// Create a dummy file for source
	err := os.WriteFile(source, []byte("test content"), 0644)
	if err != nil {
		t.Fatal("Failed to create source file:", err)
	}
	defer os.Remove(source)

	// Create symlink
	err = os.Symlink(source, dest)
	if err != nil {
		t.Fatal("Failed to create symlink:", err)
	}
	defer os.Remove(dest)

	// Test if it's a symlink
	isLink := fileutil.IsSymlink(dest)
	assert.NoError(t, err)
	assert.True(t, isLink)
}

// Test fileExists function
func TestFileExists(t *testing.T) {
	source := "./test_source.txt"

	// Create a dummy file
	err := os.WriteFile(source, []byte("test content"), 0644)
	if err != nil {
		t.Fatal("Failed to create source file:", err)
	}
	defer os.Remove(source)

	// Test file exists
	exists := fileutil.IsRegularFile(source)
	assert.True(t, exists)

	// Test file does not exist
	exists = fileutil.IsRegularFile("./non_existent.txt")
	assert.False(t, exists)
}

// Test gatherStats function
func TestGatherStats(t *testing.T) {
	sourceDir := "./test_source_dir"
	targetDir := "./test_target_dir"

	// Create source and target directories
	err := os.MkdirAll(sourceDir, 0755)
	if err != nil {
		t.Fatal("Failed to create source directory:", err)
	}
	defer os.RemoveAll(sourceDir)

	err = os.MkdirAll(targetDir, 0755)
	if err != nil {
		t.Fatal("Failed to create target directory:", err)
	}
	defer os.RemoveAll(targetDir)

	// Create a dummy file in the source directory
	sourceFile := filepath.Join(sourceDir, "test.txt")
	err = os.WriteFile(sourceFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatal("Failed to create source file:", err)
	}
	defer os.Remove(sourceFile)

	// Test gathering stats
	stats, err := gatherStats(sourceDir, targetDir, []string{})
	assert.NoError(t, err)

	// Assert that stats are correct (no symlinks, no target)
	assert.Equal(t, 0, stats.Linked)
	assert.Equal(t, 1, stats.Unlinked)
	assert.Equal(t, 0, stats.SameContents)
	assert.Equal(t, 0, stats.DifferentContents)
	assert.Equal(t, 0, stats.IncorrectSymlink)
	assert.Equal(t, 1, stats.NoTarget)
	assert.Equal(t, 0, stats.Ignored)
}
