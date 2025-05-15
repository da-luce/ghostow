package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"ghostow/fileutil"
	"ghostow/stringutil"

	"github.com/BurntSushi/toml"
	"github.com/alexflint/go-arg"
	"github.com/fatih/color"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	LogLevel   string   `toml:"log_level"`
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
		LogLevel:   "debug",
	},
}

// Logging
var sugar *zap.SugaredLogger

func InitLogger(logLevel string) error {
	// Create zap config independently
	zapCfg := zap.NewProductionConfig()
	level := zap.InfoLevel
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		log.Printf("Invalid log level %q, defaulting to info", logLevel)
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)
	zapCfg.Encoding = "console"
	zapCfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("15:04:05"))
	}
	zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapCfg.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	logger, err := zapCfg.Build()
	if err != nil {
		log.Fatalf("Failed to build logger: %v", err)
	}
	defer logger.Sync()
	sugar = logger.Sugar()
	sugar.Debug("Initialized logger.")
	return nil
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

// Common logic for walking the source directory
// walkSourceDir walks the sourceDir and calls handler for each non-ignored file or directory.
//
// Parameters:
// - sourceDirAbs: the root directory to start walking from. Absolute path.
// - ignoreList: list of filename patterns to skip (e.g., ".git", "*.tmp").
// - handler: callback function called with each file's absolute path, os.FileInfo, and relative path.
//
// The walk skips the root directory itself and any ignored files or folders.
func walkSourceDir(sourceDir string, ignoreList []string, handler func(source string, info os.FileInfo, relativePath string) error) error {

	// Ensure sourceDir is valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}
	if !fileutil.IsDir(sourceDir) {
		return fmt.Errorf("walkSourceDir: directory does not exist: %s", sourceDir)
	}

	return filepath.Walk(sourceDir, func(sourcePath string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", sourcePath, err)
			return err
		}

		// Skip the root directory (but walk into it)
		isRootDir, err := fileutil.PathsEqual(sourcePath, sourceDir)
		if err != nil {
			return fmt.Errorf("failed to compare paths: %w", err)
		}
		if isRootDir {
			return nil
		}

		// Ignore any directories or files in the ignore list
		shouldIgnore, err := fileutil.MatchesPatterns(info.Name(), ignoreList)
		if err != nil {
			return fmt.Errorf("error checking ignore patterns: %v", err)
		}
		if shouldIgnore {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle the current item in the source directory
		relativePath, err := filepath.Rel(sourceDir, sourcePath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		if err := handler(sourcePath, info, relativePath); err != nil {
			return err
		}

		if info.IsDir() {
			return filepath.SkipDir // Skip walking into sub directories (whole directories are linked)
		} else {
			return nil
		}
	})
}

// Walk the source directory and process symlinks
func createSymlinks(sourceDir, targetDir string, force, createDirs, confirm bool, ignoreList []string) error {

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string) error {

		targetAbs := filepath.Join(targetDir, sourceRel)

		// If target exists, check if it's already a correct symlink
		green := color.New(color.FgGreen).SprintFunc()
		if fileutil.PathExists(targetAbs) {
			sugar.Debugf("Target already exists: %s", targetAbs)
			linked, err := fileutil.IsSymlinkPointingTo(targetAbs, sourceAbs)
			if err != nil {
				sugar.Infof("Failed to verify symlink %s: %v", linkString(targetAbs, sourceAbs), err)
			} else if linked {
				sugar.Infof("%s%s", green("Correct symlink already exists: "), linkString(targetAbs, sourceAbs))
				return nil // Skip relinking
			}
		}

		// Check if there is an existing symlink or file
		sugar.Debugf("No link exists for %s", linkString(targetAbs, sourceAbs))
		if fileutil.PathExists(targetAbs) {
			if force {
				if err := os.RemoveAll(targetAbs); err != nil {
					return fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
				}
			} else {
				if stringutil.AskForConfirmation("Preview diff of existing file at " + targetAbs + "?") {
					PreviewDiff(sourceAbs, targetAbs)
				}
				if stringutil.AskForConfirmation("Delete existing file at " + targetAbs + "?") {
					if err := os.RemoveAll(targetAbs); err != nil {
						return fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
					}
				} else {
					fmt.Printf("Skipped: %s\n", targetAbs)
					return nil
				}
			}
		} else {
			sugar.Debugf("Target path does not exist: %s", targetAbs)
		}

		// Create the symlink
		if err := fileutil.CreateSymlink(sourceAbs, targetAbs, createDirs); err != nil {
			sugar.Infof("Error creating symlink %s: %v", linkString(sourceAbs, targetAbs), err)
		} else {
			sugar.Infof("Linked %s", linkString(sourceAbs, targetAbs))
		}

		return nil
	})

	return err
}

// Walk the target directory and remove symlinks
func removeSymlinks(sourceDir, targetDir string, ignoreList []string, confirm bool) error {

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string) error {

		targetAbs := filepath.Join(targetDir, sourceRel)

		// Skip non-symlink files (we only want symlinks)
		if !fileutil.IsSymlink(targetAbs) {
			return nil
		}

		// Ask for confirmation if needed
		if confirm && !stringutil.AskForConfirmation(fmt.Sprintf("Remove symlink %s?", linkString(sourceAbs, targetAbs))) {
			return nil
		}

		// Remove the symlink
		if err := os.Remove(targetAbs); err != nil {
			sugar.Infof("Error removing symlink %s: %v", linkString(targetAbs, sourceAbs), err)
		} else {
			sugar.Infof("Removed symlink: %s", linkString(targetAbs, sourceAbs))
		}

		return nil
	})

	return err
}

type Stats struct {
	LinkedFiles       int
	LinkedDirs        int
	Unlinked          int
	SameContents      int
	DifferentContents int
	IncorrectSymlink  int
	NoTarget          int
	Ignored           int
}

func gatherStats(sourceDir string, targetDir string, ignoreList []string) (Stats, error) {
	stats := Stats{}

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return stats, fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return stats, fmt.Errorf("walkSourceDir: expected absolute path, got: %s", sourceDir)
	}

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string) error {

		targetAbs := filepath.Join(targetDir, sourceRel)

		// Check if the target path exists for this source
		// IMPORTANT: returns if a symlink!
		if !fileutil.PathExists(targetAbs) {
			stats.NoTarget++
			stats.Unlinked++
			sugar.Debugf("Target path %s does not exist\n", targetAbs)
			return nil
		}

		// Check if it is a symlink
		isLink := fileutil.IsSymlink(targetAbs)
		if !isLink {
			different, err := fileutil.CompareFileHashes(sourceAbs, targetAbs)
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
		targetDest, err := os.Readlink(targetAbs)
		if err != nil {
			return fmt.Errorf("error reading symlink: %v", err)
		}

		correctSource := fileutil.ExpandPath(targetDest) == fileutil.ExpandPath(sourceAbs)
		if correctSource {
			if fileutil.IsDir(targetDest) {
				stats.LinkedDirs++
			} else {
				stats.LinkedFiles++
			}
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
		sugar.Fatalf("Error gathering stats: %v", err)
	}
	fmt.Printf("Displaying statistics for linking %s\n\n", linkString(sourceDir, targetDir))
	rows := [][2]string{
		{"Linked files", green(stats.LinkedFiles)},
		{"Linked directories", green(stats.LinkedDirs)},
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

	// Parse config
	if fileutil.IsRegularFile(args.ConfigFile) {
		if _, err := toml.DecodeFile(args.ConfigFile, &cfg); err != nil {
			sugar.Fatalf("Failed to parse config: %v", err)
			return
		}

	}
	InitLogger(cfg.Options.LogLevel)

	// Parse config file
	if !fileutil.IsRegularFile(args.ConfigFile) {
		sugar.Infof("No config file found at %s. Using default config.", args.ConfigFile)
	} else {
		sugar.Infof("Using config at %s", args.ConfigFile)
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
		sugar.Infof("Using source directory %s", sourceDir)
	}
	if !fileutil.IsDir(targetDir) {
		fmt.Printf("Target directory %s not found\n", targetDir)
		return
	} else {
		sugar.Infof("Using target directory %s", targetDir)
	}

	sourceDirAbs, _ := filepath.Abs(sourceDir)

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
	if fileutil.IsRegularFile(ignoreFile) {
		additionalIgnores, err := fileutil.ReadFileLines(ignoreFile, ignoreBlank)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", ignoreFile, err)
			return
		}
		cfg.Options.Ignore = append(cfg.Options.Ignore, additionalIgnores...)
		sugar.Debugf("Adding additional ignore rules: %s", additionalIgnores)
	} else {
		sugar.Debugf("No ignore file found")
	}

	// Handle arguments
	switch args.Command {
	case "link":
		if err := createSymlinks(sourceDirAbs, targetDir, cfg.Options.Force, cfg.Options.CreateDirs, cfg.Options.Confirm, cfg.Options.Ignore); err != nil {
			sugar.Fatalf("Error linking: %v", err)
		}

	case "unlink":
		if err := removeSymlinks(sourceDirAbs, targetDir, cfg.Options.Ignore, cfg.Options.Confirm); err != nil {
			sugar.Fatalf("Error unlinking: %v", err)
		}

	case "stats":
		printStats(sourceDirAbs, targetDir, cfg.Options.Ignore)

	default:
		fmt.Println("Unknown command:", args.Command)
	}
}
