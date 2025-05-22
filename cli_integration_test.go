package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
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
