package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"lnkit/ymlfs"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLinkCommand_CreatesSymlink_FtoF(t *testing.T) {
	InitLogger("Fatal")
	// Setup temp directories and files
	tmpDir := t.TempDir()
	linkPath := filepath.Join(tmpDir, "mylink")
	targetPath := filepath.Join(tmpDir, "mytarget")

	// Create target file
	err := os.WriteFile(targetPath, []byte("hello"), 0644)
	require.NoError(t, err)

	// Build root command and add the real `link` subcommand
	rootCmd := &cobra.Command{Use: "lnk"}
	rootCmd.AddCommand(NewLinkCmd())

	// Capture output
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Set args to simulate: lnk link linkPath targetPath
	rootCmd.SetArgs([]string{"link", linkPath, targetPath})

	// Run command
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Assert symlink was created
	fi, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, fi.Mode()&os.ModeSymlink != 0, "Expected a symlink")

	resolved, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, targetPath, resolved)
}

func TestLinkCommand_CreatesSymlink_DtoD(t *testing.T) {
	InitLogger("Fatal")

	// Setup temp root dir
	tmpDir := t.TempDir()
	linkPath := filepath.Join(tmpDir, "mylinkdir")
	targetPath := filepath.Join(tmpDir, "mytargetdir")

	// Create target directory
	err := os.Mkdir(targetPath, 0755)
	require.NoError(t, err)

	// Build root command and add the real `link` subcommand
	rootCmd := &cobra.Command{Use: "lnk"}
	rootCmd.AddCommand(NewLinkCmd())

	// Capture output
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Set args to simulate: lnk link linkPath targetPath
	rootCmd.SetArgs([]string{"link", linkPath, targetPath})

	// Run command
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Assert symlink was created
	fi, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, fi.Mode()&os.ModeSymlink != 0, "Expected a symlink")

	resolved, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, targetPath, resolved)
}

func TestLinkCommand_CreatesSymlink_RecSimple(t *testing.T) {
	InitLogger("Fatal")

	tmpDir := t.TempDir()

	// YAML description of the directory actualFs
	actualFs := `
file1.txt: null
config: {}
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
`

	// Use your FromYml helper to create the directory structure on disk
	err := ymlfs.FromYml(tmpDir, []byte(actualFs))
	require.NoError(t, err)

	linkPath := filepath.Join(tmpDir, "config")
	targetPath := filepath.Join(tmpDir, ".dotfiles")

	rootCmd := &cobra.Command{Use: "lnk"}
	rootCmd.AddCommand(NewLinkCmd())

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	rootCmd.SetArgs([]string{"link", "--rec", linkPath, targetPath})

	err = rootCmd.Execute()
	require.NoError(t, err)

	expectedFs := `
file1.txt: null
config:
    file2.txt:
        symlink: ../.dotfiles/file2.txt
    dirB:
        file3.txt:
            symlink: ../../.dotfiles/dirB/file3.txt
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
`

	// Read back the directory structure from disk to compare
	actualYamlBytes, err := ymlfs.ToYml(tmpDir)
	require.NoError(t, err)

	// Unmarshal expected YAML into map for comparison
	var expectedMap map[string]interface{}
	err = yaml.Unmarshal([]byte(expectedFs), &expectedMap)
	require.NoError(t, err)

	// Unmarshal actual YAML into map for comparison
	var actualMap map[string]interface{}
	err = yaml.Unmarshal(actualYamlBytes, &actualMap)
	require.NoError(t, err)

	// Compare the expected and actual directory trees
	assert.Equal(t, expectedMap, actualMap)

	// You can add assertions here for the symlink(s) if needed
}
