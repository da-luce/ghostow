package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"ghostow/fileutil"
	"ghostow/stringutil"

	"github.com/BurntSushi/toml"
	"github.com/alexflint/go-arg"
	"github.com/fatih/color"
)

type Config struct {
	Options Options           `toml:"options"`
	Links   map[string]string `toml:"exceptions"` // Custom exceptions as source -> target mappings
}

type Options struct {
	Confirm    bool     `toml:"confirm"`
	Force      bool     `toml:"force"`
	CreateDirs bool     `toml:"create_dirs"`
	SourceDir  string   `toml:"source_dir"`
	TargetDir  string   `toml:"target_dir"`
	Ignore     []string `toml:"ignore"`
}

// Default configuration to fall back on if no config file is found
var defaultConfig = Config{
	Options: Options{
		Confirm:    true,
		Force:      false,
		CreateDirs: true,
		SourceDir:  ".",
		TargetDir:  "~",
		Ignore:     []string{"ghostow.toml", ".ghostowignore", "*.git"},
	},
}

func linkString(source string, dest string) string {
	blue := color.New(color.FgBlue).SprintFunc()
	return blue(fmt.Sprintf("%s → %s", source, dest))
}

// PreviewDiff runs git diff between two files
func PreviewDiff(source, target string) error {
	cmd := exec.Command("git", "--no-pager", "diff", "--color", "--no-index", source, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Walk the source directory and process symlinks
func createSymlinks(sourceDir, targetDir string, force, createDirs, confirm bool, ignoreList []string) error {
	// Walk the source directory
	err := filepath.Walk(sourceDir, func(source string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip ignores
		shouldIgnore, err := fileutil.MatchesPatterns(info.Name(), ignoreList)
		if err != nil {
			return fmt.Errorf("error checking ignore patterns: %v", err)
		}
		if shouldIgnore {
			if info.IsDir() {
				return filepath.SkipDir // Skip walking into the directory
			} else {
				return nil // Continue walking without processing this file
			}
		}

		// Build the relative path from the source directory
		relativePath, _ := filepath.Rel(sourceDir, source)
		destAbs := fileutil.ExpandPath(filepath.Join(targetDir, relativePath))
		source = fileutil.ExpandPath(source)
		sourceAbs, err := filepath.Abs(source)

		if relativePath == "." {
			// This is the top-level source directory itself, skip linking it
			// Just continue walking inside it
			return nil
		}

		if fileutil.PathExists(destAbs) {
			log.Printf("Path dest already exists %s", destAbs)
			linked, err := fileutil.IsSymlinkPointingTo(destAbs, sourceAbs)
			if err != nil {
				log.Printf("Error checking symlink for %s: %v", destAbs, err)
			} else if linked {
				log.Printf("Link already exists for %s -> %s", destAbs, sourceAbs)
				return nil // skip creating link again
			} else {
				log.Printf("No link exists for %s -> %s", destAbs, sourceAbs)
			}

			// Ask for confirmation if needed
			if confirm && !stringutil.AskForConfirmation(linkString(sourceAbs, destAbs)) {
				return nil
			}

			// Remove the existing symlink or file if needed
			if fileutil.PathExists(destAbs) {
				if force {
					if err := os.RemoveAll(destAbs); err != nil {
						return fmt.Errorf("failed to remove existing file %s: %w", destAbs, err)
					}
				} else {
					if stringutil.AskForConfirmation("Preview diff of existing file at " + destAbs + "?") {
						PreviewDiff(source, destAbs)
					}
					if stringutil.AskForConfirmation("Delete existing file at " + destAbs + "?") {
						if err := os.RemoveAll(destAbs); err != nil {
							return fmt.Errorf("failed to remove existing file %s: %w", destAbs, err)
						}
					} else {
						fmt.Printf("Skipped: %s\n", destAbs)
						return nil
					}
				}
			}
		} else {
			log.Printf("Path does not exist %s", destAbs)
		}

		// Create the symlink
		if err := fileutil.CreateSymlink(sourceAbs, destAbs, createDirs); err != nil {
			log.Printf("Error creating symlink for %s: %v", source, err)
		} else {
			fmt.Printf("Linked %s -> %s\n", sourceAbs, destAbs)
		}

		if info.IsDir() {
			return filepath.SkipDir // Skip walking into the directory
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
		source := fileutil.ExpandPath(filepath.Join(sourceDir, relativePath)) // Construct source path

		// Ask for confirmation if needed
		blue := color.New(color.FgBlue).SprintFunc()
		link := blue(fmt.Sprintf("%s -> %s", source, target))
		if confirm && !stringutil.AskForConfirmation(fmt.Sprintf("Remove symlink %s?", link)) {
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

type Stats struct {
	Linked            int
	Unlinked          int
	SameContents      int
	DifferentContents int
	IncorrectSymlink  int
	NoTarget          int
	Ignored           int
}

func gatherStats(sourceDir string, targetDir string, ignoreList []string) (Stats, error) {
	stats := Stats{}

	err := filepath.Walk(sourceDir, func(sourcePath string, info os.FileInfo, err error) error {

		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", sourcePath, err)
			return err
		}

		relPath, _ := filepath.Rel(sourceDir, sourcePath)
		targetPath := filepath.Join(targetDir, relPath)

		shouldIgnore, err := fileutil.MatchesPatterns(info.Name(), ignoreList)
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
		if !fileutil.PathExists(targetPath) {
			stats.NoTarget++
			stats.Unlinked++
			return nil
		}

		// Check if it is a symlink
		isLink := fileutil.IsSymlink(targetPath)
		if !isLink {
			different, err := fileutil.CompareFileHashes(sourcePath, targetPath)
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

		correctSource := fileutil.ExpandPath(linkedTarget) == fileutil.ExpandPath(sourcePath)
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
	ConfigFile string `arg:"-c,--config" help:"path to config file" default:"ghostow.toml"`
	TargetDir  string `arg:"-t,--target" help:"Override target directory"`
	SourceDir  string `arg:"-s,--source" help:"Override source directory"`
}

func printStats(sourceDir string, targetDir string, ignore []string) {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()
	stats, err := gatherStats(sourceDir, targetDir, ignore)
	if err != nil {
		log.Fatalf("Error gathering stats: %v", err)
	}
	fmt.Printf("Displaying statistics for linking %s\n\n", linkString(sourceDir, targetDir))
	rows := [][2]string{
		{"Linked files", green(stats.Linked)},
		{"Unlinked files", red(stats.Unlinked)},
		{"  ├─ Target does not exist", red(stats.NoTarget)},
		{"  ├─ Target is broken link", red(stats.IncorrectSymlink)},
		{"  ├─ Target exists with same content", red(stats.SameContents)},
		{"  ╰─ Target exists with different content", red(stats.DifferentContents)},
		{"Ignored files", blue(stats.Ignored)},
	}
	stringutil.PrintDotTable(rows)
}

const ignoreFile = ".ghostowignore"
const configFile = "ghostow.toml"

func main() {
	var args Args
	arg.MustParse(&args)

	// Load config
	var cfg Config = defaultConfig

	// Parse config file
	if !fileutil.IsRegularFile(args.ConfigFile) {
		log.Printf("No config file found at %s. Using default config.\n", args.ConfigFile)
	} else {
		if _, err := toml.DecodeFile(args.ConfigFile, &cfg); err != nil {
			log.Fatalf("Failed to parse config: %v", err)
			return
		}
		log.Printf("Using config at %s\n", args.ConfigFile)
	}

	// Expand and override source/target dirs from CLI args if provided
	if args.SourceDir != "" {
		cfg.Options.SourceDir = args.SourceDir
	}
	if args.TargetDir != "" {
		cfg.Options.TargetDir = args.TargetDir
	}

	// Parse source and target directories
	sourceDir := fileutil.ExpandPath(cfg.Options.SourceDir)
	targetDir := fileutil.ExpandPath(cfg.Options.TargetDir)
	if !fileutil.IsDir(sourceDir) {
		fmt.Printf("Source directory %s not found\n", sourceDir)
		return
	} else {
		log.Printf("Using source directory %s\n", sourceDir)
	}
	if !fileutil.IsDir(targetDir) {
		fmt.Printf("Target directory %s not found\n", targetDir)
		return
	} else {
		log.Printf("Using target directory %s\n", targetDir)
	}

	// Ensure target dir is not a child of the source
	isChild, err := fileutil.IsChildPath(targetDir, sourceDir)
	if err != nil {
		fmt.Printf("Error checking path relationship: %v\n", err)
		return
	}
	if isChild {
		fmt.Printf("Target directory %s is a child of source %s\n", targetDir, sourceDir)
		return
	}

	// Add additional ignore rules
	ignoreBlank := true
	if !fileutil.IsRegularFile(ignoreFile) {
		additionalIgnores, err := fileutil.ReadFileLines(ignoreFile, ignoreBlank)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", ignoreFile, err)
			return
		}
		cfg.Options.Ignore = append(cfg.Options.Ignore, additionalIgnores...)
		log.Println("Adding additional ignore rules:", additionalIgnores)
	}

	// Handle arguments
	switch args.Command {
	case "link":
		if err := createSymlinks(sourceDir, targetDir, cfg.Options.Force, cfg.Options.CreateDirs, cfg.Options.Confirm, cfg.Options.Ignore); err != nil {
			log.Fatalf("Error linking: %v", err)
		}

	case "unlink":
		if err := removeSymlinks(sourceDir, targetDir, cfg.Options.Force); err != nil {
			log.Fatalf("Error unlinking: %v", err)
		}

	case "stats":
		printStats(sourceDir, targetDir, cfg.Options.Ignore)

	default:
		fmt.Println("Unknown command:", args.Command)
	}
}
