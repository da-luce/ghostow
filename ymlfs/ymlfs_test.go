package ymlfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// applyAndCheckRoundTrip creates a structure from YAML, re-serializes it, and checks equality.
// This effectively tests that our serialization and deserialization are self-consistent
func applyAndCheckRoundTrip(t *testing.T, yamlData []byte, tmpDir string) {
	t.Helper()

	err := FromYml(tmpDir, yamlData)
	require.NoError(t, err)

	outYaml, err := ToYml(tmpDir)
	require.NoError(t, err)

	got, err := ToMap(outYaml)
	require.NoError(t, err)

	want, err := ToMap(yamlData)
	require.NoError(t, err)

	require.Equal(t, want, got)
}

// requireSymlink asserts that a given path is a symlink pointing to the expected target.
func requireSymlink(t *testing.T, linkPath, expectedTarget string) {
	t.Helper()

	info, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, info.Mode()&os.ModeSymlink != 0, "Expected symlink at %s", linkPath)

	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, expectedTarget, target)
}

func TestFromYmlAndToYml_SingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	yamlData := []byte(`file.txt: null`)
	applyAndCheckRoundTrip(t, yamlData, tmpDir)
	require.FileExists(t, filepath.Join(tmpDir, "file.txt"))
}

func TestFromYmlAndToYml_SingleDir(t *testing.T) {
	tmpDir := t.TempDir()
	yamlData := []byte(`mydir: {}`)
	applyAndCheckRoundTrip(t, yamlData, tmpDir)
	info, err := os.Stat(filepath.Join(tmpDir, "mydir"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestFromYmlAndToYml_SingleSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create real target
	err := os.WriteFile(filepath.Join(tmpDir, "real.txt"), []byte("data"), 0644)
	require.NoError(t, err)

	yamlData := []byte(`
real.txt: null
link.txt:
  symlink: real.txt
`)
	applyAndCheckRoundTrip(t, yamlData, tmpDir)
	require.FileExists(t, filepath.Join(tmpDir, "real.txt"))
	requireSymlink(t, filepath.Join(tmpDir, "link.txt"), "real.txt")
}

func TestFromYmlAndToYml(t *testing.T) {
	tmpDir := t.TempDir()

	yamlData := []byte(`
file1.txt: null
config: {}
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
link_to_file1:
  symlink: file1.txt
link_to_dirB:
  symlink: .dotfiles/dirB
`)
	applyAndCheckRoundTrip(t, yamlData, tmpDir)

	require.FileExists(t, filepath.Join(tmpDir, "file1.txt"))
	require.DirExists(t, filepath.Join(tmpDir, "config"))
	require.FileExists(t, filepath.Join(tmpDir, ".dotfiles", "file2.txt"))
	require.FileExists(t, filepath.Join(tmpDir, ".dotfiles", "dirB", "file3.txt"))

	requireSymlink(t, filepath.Join(tmpDir, "link_to_file1"), "file1.txt")
	requireSymlink(t, filepath.Join(tmpDir, "link_to_dirB"), ".dotfiles/dirB")
}
