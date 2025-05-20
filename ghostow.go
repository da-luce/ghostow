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
	sugar.Debug("Initialized logger")
	return nil
}

func linkString(source string, dest string) string {
	blue := color.New(color.FgBlue).SprintFunc()
	return blue(fmt.Sprintf("%s → %s", source, dest))
}

// PreviewDiff runs git diff between two files
func PreviewDiff(source, target string) error {
	cmd := exec.Command("git", "diff", "--color", "--no-index", source, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type TargetState int

const (
	Ignore            TargetState = iota // File should be ignored
	AlreadyLinked                        // Correct symlink exists
	Missing                              // No file or link exists at target
	MislinkedInternal                    // Symlink exists but points to wrong place in source dir
	MislinkedExternal                    // Symlink exists but points outside source dir
	ExistsIdentical                      // Regular file or dir exists, content matches source
	ExistsModified                       // Regular file or dir exists, content differs from source
)

func determineTargetState(sourceDir, targetDir, sourceRel string, ignoreList []string) (TargetState, error) {

	// Get absolute paths
	targetAbs := filepath.Join(targetDir, sourceRel)
	sourceAbs := filepath.Join(sourceDir, sourceRel)

	// Ignore any directories or files in the ignore list
	if matched, err := fileutil.MatchesPatterns(filepath.Base(sourceAbs), ignoreList); err != nil {
		return Ignore, fmt.Errorf("error checking ignore patterns: %v", err)
	} else if matched {
		return Ignore, nil
	}

	if !fileutil.PathExists(targetAbs) {
		sugar.Debugf("No target exists: %s", targetAbs)
		return Missing, nil
	}

	if fileutil.IsSymlink(targetAbs) {
		linked, _ := fileutil.IsSymlinkPointingTo(targetAbs, sourceAbs)
		if linked {
			sugar.Debugf("Target link is correct: %s", linkString(targetAbs, sourceAbs))
			return AlreadyLinked, nil
		}

		linkTarget, _ := os.Readlink(targetAbs)
		inSource, _ := fileutil.IsChildPath(linkTarget, sourceDir)
		if inSource {
			sugar.Debugf("Target link is internally mislinked: %s", linkString(targetAbs, linkTarget))
			return MislinkedInternal, nil
		}
		sugar.Debugf("Target link is externally mislinked: %s", linkString(targetAbs, linkTarget))
		return MislinkedExternal, nil
	}

	// Not a symlink — check file or dir content
	// FIXME: does this work with dirs?
	same, _ := fileutil.CompareFileHashes(sourceAbs, targetAbs)
	if same {
		sugar.Debugf("Target exists and has identical content: %s", targetAbs)
		return ExistsIdentical, nil
	}
	sugar.Debugf("Target exists and has different content: %s", targetAbs)
	return ExistsModified, nil
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
func walkSourceDir(sourceDir string, targetDir string, ignoreList []string, handler func(sourceRel, targetAbs string, targetState TargetState) error) error {

	// Ensure sourceDir is valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got source directory: %s", sourceDir)
	}

	return filepath.Walk(sourceDir, func(sourceAbs string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", sourceAbs, err)
			return err
		}

		// Skip the root directory (but walk into it)
		isRootDir, err := fileutil.PathsEqual(sourceAbs, sourceDir)
		if err != nil {
			return fmt.Errorf("failed to compare paths: %w", err)
		}
		if isRootDir {
			return nil
		}

		// Ignore symlinks in the source directory
		if fileutil.IsSymlink(sourceAbs) {
			return nil
		}

		sourceRel, _ := filepath.Rel(sourceDir, sourceAbs)
		targetState, err := determineTargetState(sourceDir, targetDir, sourceRel, ignoreList)
		if err != nil {
			return err
		}

		targetAbs := filepath.Join(targetDir, sourceRel)
		if err := handler(sourceRel, targetAbs, targetState); err != nil {
			return err
		}

		// Skip walking into subdirectories (whole directories are linked)
		if info.IsDir() {
			return filepath.SkipDir
		}

		return nil
	})
}

func symlink(sourceAbs string, targetAbs string, createDirs bool) {
	if err := fileutil.CreateSymlink(sourceAbs, targetAbs, createDirs); err != nil {
		sugar.Infof("Error creating symlink %s: %v", linkString(targetAbs, sourceAbs), err)
	} else {
		sugar.Infof("Linked %s", linkString(targetAbs, sourceAbs))
	}
}

// Walk the source directory and process symlinks
func createSymlinks(sourceDir, targetDir string, force, createDirs, confirm bool, ignoreList []string) error {

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("createSymlinks: expected absolute path, got source directory: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return fmt.Errorf("createSymlinks: expected absolute path, got target directory: %s", targetDir)
	}

	err := walkSourceDir(sourceDir, targetDir, ignoreList, func(sourceRel, targetAbs string, targetState TargetState) error {

		if targetState == Ignore {
			return nil
		}

		sourceAbs := filepath.Join(sourceDir, sourceRel)

		// TODO: factor this out to be more reusable
		switch targetState {
		case Ignore, AlreadyLinked:
		case Missing:
			sugar.Debugf("Creating link %s", linkString(targetAbs, sourceAbs))
			symlink(sourceAbs, targetAbs, createDirs)
			return nil
		case MislinkedInternal:
			sugar.Debugf("Target file is broken. Creating correct symlink...")
			if err := os.RemoveAll(targetAbs); err != nil {
				return fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
			}
			symlink(sourceAbs, targetAbs, createDirs)
			return nil

		case MislinkedExternal:
			if force {
				sugar.Infof("Overwriting existing file at: ", targetAbs)
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

		case ExistsIdentical:
			sugar.Debugf("Target file has the same content. Creating correct symlink...")
			if err := os.RemoveAll(targetAbs); err != nil {
				return fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
			}
			symlink(sourceAbs, targetAbs, createDirs)
			return nil

		case ExistsModified:
			if force {
				sugar.Infof("Overwriting existing file at: ", targetAbs)
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

		default:
			// Handle unexpected state
		}

		return nil
	})

	return err
}

// Walk the target directory and remove symlinks
func removeSymlinks(sourceDir, targetDir string, ignoreList []string, confirm bool) error {

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("removeSymlinks: expected absolute path, got source directory: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return fmt.Errorf("removeSymlinks: expected absolute path, got target directory: %s", targetDir)
	}

	err := walkSourceDir(sourceDir, targetDir, ignoreList, func(sourceRel, targetAbs string, targetState TargetState) error {

		// TODO: factor this out to be more reusable
		sourceAbs := filepath.Join(sourceDir, sourceRel)

		switch targetState {
		case Ignore, Missing, ExistsIdentical, ExistsModified:
		case AlreadyLinked, MislinkedInternal:
			// Ask for confirmation if needed
			if confirm && !stringutil.AskForConfirmation(fmt.Sprintf("Remove symlink %s?", linkString(targetAbs, sourceAbs))) {
				return nil
			}

			// Remove the symlink
			if err := os.Remove(targetAbs); err != nil {
				sugar.Infof("Error removing symlink %s: %v", linkString(targetAbs, sourceAbs), err)
			} else {
				sugar.Infof("Removed symlink: %s", linkString(targetAbs, sourceAbs))
			}

		case MislinkedExternal:
			// Ask for confirmation if needed
			if confirm && !stringutil.AskForConfirmation(fmt.Sprintf("Remove symlink %s?", linkString(targetAbs, sourceAbs))) {
				return nil
			}

			// Remove the symlink
			if err := os.Remove(targetAbs); err != nil {
				sugar.Infof("Error removing symlink %s: %v", linkString(targetAbs, sourceAbs), err)
			} else {
				sugar.Infof("Removed symlink: %s", linkString(targetAbs, sourceAbs))
			}

		default:
			// Handle unexpected state
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

func gatherStats(sourceDir, targetDir string, ignoreList []string) (Stats, error) {
	stats := Stats{}

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return stats, fmt.Errorf("gatherStats: expected absolute path, got source directory: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return stats, fmt.Errorf("gatherStats: expected absolute path, got target directory: %s", targetDir)
	}

	err := walkSourceDir(sourceDir, targetDir, ignoreList, func(sourceRel, targetAbs string, targetState TargetState) error {

		switch targetState {
		case Ignore:
			stats.Ignored++
		case Missing:
			stats.NoTarget++
			stats.Unlinked++
		case AlreadyLinked:
			stats.LinkedDirs++
			stats.LinkedFiles++
		case MislinkedInternal:
			stats.IncorrectSymlink++
		case MislinkedExternal:
			stats.IncorrectSymlink++
		case ExistsIdentical:
			stats.SameContents++
		case ExistsModified:
			stats.DifferentContents++
		default:
			// Handle unexpected state
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
	fmt.Printf("Displaying statistics for linking %s\n\n", linkString(targetDir, sourceDir))
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

func main() {

	var args Args
	arg.MustParse(&args)

	// Parse config
	var cfg Config = defaultConfig
	if fileutil.IsRegularFile(args.ConfigFile) {
		if _, err := toml.DecodeFile(args.ConfigFile, &cfg); err != nil {
			sugar.Fatalf("Failed to parse config: %v", err)
			return
		}
	}

	// Initialize logging
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
	sourceDir, _ := fileutil.ExpandPath(cfg.Options.SourceDir)
	targetDir, _ := fileutil.ExpandPath(cfg.Options.TargetDir)
	// Ensure directories exist
	if !fileutil.IsDir(sourceDir) {
		fmt.Printf("Source directory %s not found\n", sourceDir)
		return
	}
	if !fileutil.IsDir(targetDir) {
		fmt.Printf("Target directory %s not found\n", targetDir)
		return
	}
	// Ensure directories aren't a link
	if fileutil.IsSymlink(sourceDir) {
		fmt.Printf("Source directory %s must not be a symlink\n", sourceDir)
		return
	}
	if fileutil.IsSymlink(targetDir) {
		fmt.Printf("Target directory %s must not be a symlink\n", targetDir)
		return
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
	sugar.Infof("Using source directory %s", sourceDir)
	sugar.Infof("Using target directory %s", targetDir)

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
		if err := createSymlinks(sourceDir, targetDir, cfg.Options.Force, cfg.Options.CreateDirs, cfg.Options.Confirm, cfg.Options.Ignore); err != nil {
			sugar.Fatalf("Error linking: %v", err)
		}

	case "unlink":
		if err := removeSymlinks(sourceDir, targetDir, cfg.Options.Ignore, cfg.Options.Confirm); err != nil {
			sugar.Fatalf("Error unlinking: %v", err)
		}

	case "stats":
		printStats(sourceDir, targetDir, cfg.Options.Ignore)

	default:
		fmt.Println("Unknown command:", args.Command)
	}
}
