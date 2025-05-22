package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"lnkit/ymlfs"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func buildRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "lnk"}
	rootCmd.AddCommand(NewLinkCmd())
	return rootCmd
}

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) string {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()
	require.NoError(t, err)

	return out.String()
}

func assertSymlink(t *testing.T, linkPath, expectedTarget string) {
	t.Helper()

	info, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, info.Mode()&os.ModeSymlink != 0, "Expected a symlink")

	actualTarget, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, expectedTarget, actualTarget)
}

func testLinkCommand(t *testing.T, initialYAML, expectedYAML []byte, cmdName, linkPath, targetPath string, args ...string) {
	InitLogger("Fatal")

	// Put into a temp dir--relativize the link and target path
	tmpDir := t.TempDir()
	linkPath = filepath.Join(tmpDir, linkPath)
	targetPath = filepath.Join(tmpDir, targetPath)

	// Create initial structure from YAML
	err := ymlfs.FromYml(tmpDir, initialYAML)
	require.NoError(t, err)

	// Prepare root command and add subcommands
	rootCmd := &cobra.Command{Use: "lnk"}
	rootCmd.AddCommand(NewLinkCmd()) // add other subcommands if needed

	// Build args: cmdName + linkPath + targetPath + any other args
	allArgs := []string{cmdName, linkPath, targetPath}
	allArgs = append(allArgs, args...)
	rootCmd.SetArgs(allArgs)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Execute command
	err = rootCmd.Execute()
	require.NoError(t, err, "command output: %s", out.String())

	// Assert final directory matches expected YAML
	ymlfs.AssertDirMatchesYAML(t, tmpDir, string(expectedYAML))
}

func TestLink_LinkFile(t *testing.T) {
	initial := []byte(`
targetfile: null
`)

	expected := []byte(`
targetfile: null
link:
  symlink: targetfile
`)

	testLinkCommand(t, initial, expected, "link", "link", "targetfile")
}

func TestLink_LinkFileRelativePath(t *testing.T) {
	initial := []byte(`
targetfile: null
`)

	expected := []byte(`
targetfile: null
link:
  symlink: targetfile
`)

	testLinkCommand(t, initial, expected, "link", "link", "./targetfile")
}

func TestLink_LinkFileNested(t *testing.T) {
	initial := []byte(`
file1: null
dir:
  targetfile: null
`)

	expected := []byte(`
file1: null
dir:
  targetfile: null
linkpath:
  symlink: dir/targetfile
`)

	testLinkCommand(t, initial, expected, "link", "linkpath", "./dir/targetfile")
}

func TestLink_LinkFileNested2(t *testing.T) {
	initial := []byte(`
file1: null
dir:
  targetfile: null
`)

	expected := []byte(`
file1: null
dir:
  targetfile: null
  linkpath:
    symlink: targetfile
`)

	testLinkCommand(t, initial, expected, "link", "./dir/linkpath", "./dir/targetfile")
}

func TestLink_LinkFileNested3(t *testing.T) {
	initial := []byte(`
file1: null
dir1:
  targetfile: null
dir2: {}
`)

	expected := []byte(`
file1: null
dir1:
  targetfile: null
dir2:
  linkpath:
    symlink: ../dir1/targetfile
`)

	testLinkCommand(t, initial, expected, "link", "./dir2/linkpath", "./dir1/targetfile")
}

func TestLink_LinkSymlink(t *testing.T) {
	initial := []byte(`
targetfile: null
1stlink:
  symlink: targetfile
`)

	expected := []byte(`
targetfile: null
1stlink:
  symlink: targetfile
2ndlink:
  symlink: 1stlink
`)

	testLinkCommand(t, initial, expected, "link", "2ndlink", "1stlink")
}

func TestLink_NoTarget(t *testing.T) {
	initial := []byte(`
targetfile: null
`)

	expected := []byte(`
targetfile: null
`)

	testLinkCommand(t, initial, expected, "link", "link", "nonexistentfile")
}

func TestLink_LinkDirectory(t *testing.T) {
	initial := []byte(`
mytargetdir: {}
`)

	expected := []byte(`
mylinkdir:
  symlink: mytargetdir
mytargetdir: {}
`)

	testLinkCommand(t, initial, expected, "link", "mylinkdir", "mytargetdir")
}

func TestLink_LinkRecursive(t *testing.T) {
	initial := []byte(`
file1.txt: null
config: {}
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
`)

	expected := []byte(`
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
`)

	testLinkCommand(t, initial, expected, "link", "config", ".dotfiles", "--rec")
}

func TestLink_LinkRecursiveFold(t *testing.T) {
	initial := []byte(`
file1.txt: null
config: {}
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
`)

	expected := []byte(`
file1.txt: null
config:
  file2.txt:
    symlink: ../.dotfiles/file2.txt
  dirB:
      symlink: ../.dotfiles/dirB
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
`)

	testLinkCommand(t, initial, expected, "link", "config", ".dotfiles", "--rec", "--fold")
}

func TestLink_Dotfiles(t *testing.T) {
	initial := []byte(`
home:
  file1.txt: null
  .dotfiles:
    file2.txt: null
    .config:
      file3.txt: null
      my_app:
        file4.txt: null
`)

	expected := []byte(`
home:
  file1.txt: null
  file2.txt:
    symlink: .dotfiles/file2.txt
  .config:
    file3.txt:
      symlink: ../.dotfiles/.config/file3.txt
    my_app:
      file4.txt:
        symlink: ../../.dotfiles/.config/my_app/file4.txt
  .dotfiles:
    file2.txt: null
    .config:
      file3.txt: null
      my_app:
        file4.txt: null
`)

	testLinkCommand(t, initial, expected, "link", "./home", "./home/.dotfiles", "--rec")
}
