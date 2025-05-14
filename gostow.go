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
)

type Config struct {
	Defaults Defaults          `toml:"defaults"`
	Links    map[string]string `toml:"exceptions"` // Custom exceptions as source -> target mappings
}

type Defaults struct {
	Confirm    bool   `toml:"confirm"`
	Force      bool   `toml:"force"`
	CreateDirs bool   `toml:"create_dirs"`
	SourceDir  string `toml:"source_dir"`
	TargetDir  string `toml:"target_dir"`
}

// Default configuration to fall back on if no config file is found
var defaultConfig = Config{
	Defaults: Defaults{
		Confirm:    true,
		Force:      false,
		CreateDirs: true,
		SourceDir:  ".",
		TargetDir:  "~",
	},
	Links: make(map[string]string),
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
	fmt.Printf("%s [y/N]: ", prompt)
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
		if confirm && !askForConfirmation(fmt.Sprintf("Are you sure you want to link %s to %s?", source, dest)) {
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

func main() {
	// Look for a config file in the current directory
	var configFile string
	if _, err := os.Stat("gostow.toml"); err == nil {
		configFile = "gostow.toml"
	} else {
		fmt.Println("No gostow.toml file found, using default settings.")
	}

	var cfg Config
	if configFile != "" {
		// Load the TOML config if the file exists
		if _, err := toml.DecodeFile(configFile, &cfg); err != nil {
			log.Fatalf("Failed to parse config: %v", err)
		}
	} else {
		// Use default config if no config file exists
		cfg = defaultConfig
	}

	// Expand the target directory path
	sourceDir := expandPath(cfg.Defaults.SourceDir)
	targetDir := expandPath(cfg.Defaults.TargetDir)

	// Walk through the source directory and create symlinks
	err := createSymlinks(sourceDir, targetDir, cfg.Defaults.Force, cfg.Defaults.CreateDirs, cfg.Defaults.Confirm)
	if err != nil {
		log.Fatalf("Error creating symlinks: %v", err)
	}
}
