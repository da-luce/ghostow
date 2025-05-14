package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alexflint/go-arg"
	"github.com/fatih/color"
)

type Config struct {
	Defaults Defaults          `toml:"defaults"`
	Links    map[string]string `toml:"exceptions"` // Custom exceptions as source -> target mappings
}

type Defaults struct {
	Confirm    bool     `toml:"confirm"`
	Force      bool     `toml:"force"`
	CreateDirs bool     `toml:"create_dirs"`
	SourceDir  string   `toml:"source_dir"`
	TargetDir  string   `toml:"target_dir"`
	Ignore     []string `toml:"ignore"`
}

// Default configuration to fall back on if no config file is found
var defaultConfig = Config{
	Defaults: Defaults{
		Confirm:    true,
		Force:      false,
		CreateDirs: true,
		SourceDir:  ".",
		TargetDir:  "~",
		Ignore:     []string{"gostow.toml", ".gostowignore", "*.git"},
	},
}

func expandPath(path string) string {
	// Expands the ~ to the full home directory path
	if strings.HasPrefix(path, "~") {
		usr, _ := user.Current()
		return filepath.Join(usr.HomeDir, path[1:])
	}
	return os.ExpandEnv(path)
}

// Create a symlink at the target location
func createSymlink(source, dest string, force, createDirs bool) error {
	// Ensure the target directory exists
	if createDirs {
		dir := filepath.Dir(dest)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Remove the existing symlink or file if needed
	if force {
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("failed to remove existing file %s: %w", dest, err)
		}
	}

	// Create the symlink
	if err := os.Symlink(source, dest); err != nil {
		return fmt.Errorf("failed to create symlink from %s to %s: %w", source, dest, err)
	}

	fmt.Printf("Linked %s -> %s\n", source, dest)
	return nil
}

// Ask for confirmation from the user
func askForConfirmation(prompt string) bool {
	bold := color.New(color.Bold).SprintFunc()
	fmt.Printf("%s [y/%s]: ", prompt, bold("N"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y"
}

func isSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// Walk the source directory and process symlinks
func createSymlinks(sourceDir, targetDir string, force, createDirs, confirm bool) error {
	// Walk the source directory
	err := filepath.Walk(sourceDir, func(source string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (we only want files)
		if info.IsDir() {
			return nil
		}

		// Build the relative path from the source directory
		relativePath, _ := filepath.Rel(sourceDir, source)
		dest := expandPath(filepath.Join(targetDir, relativePath)) // Construct destination path
		source = expandPath(source)

		// Ask for confirmation if needed
		blue := color.New(color.FgBlue).SprintFunc()
		link := blue(fmt.Sprintf("%s -> %s", source, dest))
		if confirm && !askForConfirmation(fmt.Sprintf("Link %s?", link)) {
			return nil
		}

		// Create the symlink
		if err := createSymlink(source, dest, force, createDirs); err != nil {
			log.Printf("Error creating symlink for %s: %v", source, err)
		}
		return nil
	})

	return err
}

// Walk the target directory and remove symlinks
func removeSymlinks(sourceDir, targetDir string, confirm bool) error {
	// Walk the target directory
	err := filepath.Walk(targetDir, func(target string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-symlink files (we only want symlinks)
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		// Build the relative path from the target directory
		relativePath, _ := filepath.Rel(targetDir, target)
		source := expandPath(filepath.Join(sourceDir, relativePath)) // Construct source path

		// Ask for confirmation if needed
		blue := color.New(color.FgBlue).SprintFunc()
		link := blue(fmt.Sprintf("%s -> %s", source, target))
		if confirm && !askForConfirmation(fmt.Sprintf("Remove symlink %s?", link)) {
			return nil
		}

		// Remove the symlink
		if err := os.Remove(target); err != nil {
			log.Printf("Error removing symlink for %s: %v", target, err)
		} else {
			log.Printf("Removed symlink: %s", target)
		}

		return nil
	})

	return err
}

// contains checks if the ignore list contains the given file/directory path
func contains(ignoreList []string, path string) bool {
	for _, ignorePath := range ignoreList {
		if path == ignorePath {
			return true
		}
	}
	return false
}

// ignorePath checks if the given file or directory name matches any pattern in the ignore list.
// It returns true if the path should be ignored.
func ignoreName(name string, ignore []string) (bool, error) {
	for _, pattern := range ignore {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			return false, fmt.Errorf("error matching pattern %s: %v", pattern, err)
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// True if symlink too!
func fileExists(path string) bool {
	// Use os.Lstat to get the status of the file, even if it's a symlink
	info, err := os.Lstat(path)
	return err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0)
}

func symlinkTarget(path string) (string, error) {
	return os.Readlink(path)
}

// hashFile generates a SHA-256 hash for the given file.
func hashFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %v", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", path, err)
	}

	return hash.Sum(nil), nil
}

// compareFileHashes compares the hashes of two files.
func compareFileHashes(file1, file2 string) (bool, error) {
	hash1, err := hashFile(file1)
	if err != nil {
		return false, err
	}

	hash2, err := hashFile(file2)
	if err != nil {
		return false, err
	}

	// Compare the hashes
	return !bytes.Equal(hash1, hash2), nil
}

func readFileLines(filePath string, ignoreBlank bool) ([]string, error) {
	var lines []string

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Ignore blank lines if the flag is set
		if ignoreBlank && line == "" {
			continue
		}

		// Append the line (whether it's blank or not based on the flag)
		lines = append(lines, line)
	}

	// Check for scanning errors
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	return lines, nil
}

type Stats struct {
	Linked            int
	Unlinked          int
	SameContents      int
	DifferentContents int
	IncorrectSymlink  int
	NoTarget          int
	Ignored           int
}

func gatherStats(sourceDir string, targetDir string, ignore []string) (Stats, error) {
	stats := Stats{}

	err := filepath.Walk(sourceDir, func(sourcePath string, info os.FileInfo, err error) error {

		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", sourcePath, err)
			return err
		}

		relPath, _ := filepath.Rel(sourceDir, sourcePath)
		targetPath := filepath.Join(targetDir, relPath)

		shouldIgnore, err := ignoreName(info.Name(), ignore)
		if err != nil {
			return fmt.Errorf("error checking ignore patterns: %v", err)
		}

		if shouldIgnore {
			if info.IsDir() {
				return filepath.SkipDir // Skip walking into the directory
			} else {
				stats.Ignored++
				return nil // Continue walking without processing this file
			}
		}

		// Skip other directories
		if info.IsDir() {
			return nil
		}

		// Check if the target path exists for this source
		// IMPORTANT: returns if a symlink!
		if !fileExists(targetPath) {
			stats.NoTarget++
			stats.Unlinked++
			return nil
		}

		// Check if it is a symlink
		isLink, err := isSymlink(targetPath)
		if err != nil {
			stats.Unlinked++
			return nil
		}

		if !isLink {
			different, err := compareFileHashes(sourcePath, targetPath)
			if err != nil {
				fmt.Printf("Error comparing files: %v\n", err)
			} else if different {
				stats.DifferentContents++
			} else {
				stats.SameContents++
			}
			stats.Unlinked++
			return nil
		}

		// Target is a symlink, check if it is linked to the source
		linkedTarget, err := os.Readlink(targetPath)
		if err != nil {
			return fmt.Errorf("error reading symlink: %v", err)
		}

		correctSource := expandPath(linkedTarget) == expandPath(sourcePath)
		if correctSource {
			stats.Linked++
		} else {
			stats.IncorrectSymlink++
			stats.Unlinked++
		}

		return nil
	})

	return stats, err
}

type Args struct {
	Command    string `arg:"positional,required" help:"command to run (link, unstow, stats)"`
	ConfigFile string `arg:"-c,--config" help:"path to config file" default:"gostow.toml"`
}

func areDirsValid(sourceDir, targetDir string) bool {
	// Check if sourceDir and targetDir exist and are directories
	sourceInfo, err := os.Stat(sourceDir)
	if err != nil || !sourceInfo.IsDir() {
		return false
	}

	targetInfo, err := os.Stat(targetDir)
	if err != nil || !targetInfo.IsDir() {
		return false
	}

	// Check if the directories are the same
	return sourceDir != targetDir
}

func main() {
	var args Args
	arg.MustParse(&args)

	// Load config
	var cfg Config = defaultConfig
	if !fileExists(args.ConfigFile) {
		fmt.Printf("No config file found at %s. Using default config.\n", args.ConfigFile)
	}
	if _, err := toml.DecodeFile(args.ConfigFile, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	sourceDir := expandPath(cfg.Defaults.SourceDir)
	targetDir := expandPath(cfg.Defaults.TargetDir)
	if !areDirsValid(sourceDir, targetDir) {
		fmt.Println("Target or source is bad.")
		return
	}

	ignoreFile := ".gostowignore"
	ignoreBlank := true
	if fileExists(ignoreFile) {
		additionalIgnores, err := readFileLines(ignoreFile, ignoreBlank)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", ignoreFile, err)
			return
		}
		cfg.Defaults.Ignore = append(cfg.Defaults.Ignore, additionalIgnores...)
		log.Println("Adding additional ignores:", additionalIgnores)
	}

	switch args.Command {
	case "link":
		if err := createSymlinks(sourceDir, targetDir, cfg.Defaults.Force, cfg.Defaults.CreateDirs, cfg.Defaults.Confirm); err != nil {
			log.Fatalf("Error linking: %v", err)
		}

	case "unlink":
		if err := removeSymlinks(sourceDir, targetDir, cfg.Defaults.Force); err != nil {
			log.Fatalf("Error unlinking: %v", err)
		}

	case "stats":
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()
		blue := color.New(color.FgBlue).SprintFunc()
		stats, err := gatherStats(sourceDir, targetDir, cfg.Defaults.Ignore)
		if err != nil {
			log.Fatalf("Error gathering stats: %v", err)
		}
		fmt.Printf("Linked files    %s\n", green(stats.Linked))
		fmt.Printf("Unlinked files  %s\n", red(stats.Unlinked))
		fmt.Printf("    Target does not exist                  %s\n", red(stats.NoTarget))
		fmt.Printf("    Target does not point to source        %s\n", red(stats.IncorrectSymlink))
		fmt.Printf("    Target exists with same content        %s\n", red(stats.SameContents))
		fmt.Printf("    Target exists with different content   %s\n", red(stats.DifferentContents))

		fmt.Printf("Ignored files	%s\n", blue(stats.Ignored))

	default:
		fmt.Println("Unknown command:", args.Command)
	}
}
