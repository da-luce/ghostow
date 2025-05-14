package main

import (
	"bufio"
	"fmt"
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

type Stats struct {
	Linked   int
	Unlinked int
	Ignored  int
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

func gatherStats(sourceDir string, targetDir string, ignore []string) (Stats, error) {
	stats := Stats{}

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", path, err)
			return err
		}

		relPath, _ := filepath.Rel(sourceDir, path)
		dest := filepath.Join(targetDir, relPath)

		// Skip directories
		if info.IsDir() {
			for _, pattern := range ignore {
				matched, err := filepath.Match(pattern, info.Name())
				if err != nil {
					return fmt.Errorf("error matching pattern %s: %v", pattern, err)
				}
				if matched {
					fmt.Printf("Skipping directory: %s\n", info.Name())
					return filepath.SkipDir // Skip walking into the directory
				}
			}

			return nil
		}

		fmt.Printf("Checking symlink: %s\n", dest)

		// Check for symlink
		fmt.Printf("Checking file: %s\n", dest)
		destInfo, err := os.Lstat(dest)
		if err != nil {
			stats.Unlinked++
			return nil
		}

		if destInfo.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(dest)
			if err == nil && filepath.Clean(linkTarget) == filepath.Clean(path) {
				stats.Linked++
			} else {
				stats.Unlinked++
			}
		} else {
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

func main() {
	var args Args
	arg.MustParse(&args)

	// Load config
	var cfg Config = defaultConfig // Start with the default configuration
	// Check if the config file exists
	if _, err := os.Stat(args.ConfigFile); err == nil {
		// If the file exists, parse it
		if _, err := toml.DecodeFile(args.ConfigFile, &cfg); err != nil {
			log.Fatalf("Failed to parse config: %v", err)
		}
	} else {
		fmt.Printf("No config file found at %s. Using default config.\n", args.ConfigFile)
	}

	sourceDir := expandPath(cfg.Defaults.SourceDir)
	targetDir := expandPath(cfg.Defaults.TargetDir)

	switch args.Command {
	case "link":
		if err := createSymlinks(sourceDir, targetDir, cfg.Defaults.Force, cfg.Defaults.CreateDirs, cfg.Defaults.Confirm); err != nil {
			log.Fatalf("Error linking: %v", err)
		}

	case "stats":
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()
		blue := color.New(color.FgBlue).SprintFunc()
		stats, err := gatherStats(sourceDir, targetDir, cfg.Defaults.Ignore)
		if err != nil {
			log.Fatalf("Error gathering stats: %v", err)
		}
		fmt.Printf("Linked files:   %s\n", green(stats.Linked))
		fmt.Printf("Unlinked files:	%s\n", red(stats.Unlinked))
		fmt.Printf("Ignored files:  %s\n", blue(stats.Ignored))

	default:
		fmt.Println("Unknown command:", args.Command)
	}
}
