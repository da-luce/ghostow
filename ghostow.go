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

type FileState int

const (
	Ignore            FileState = iota // File should be ignored
	AlreadyLinked                      // Correct symlink exists
	Missing                            // No file or link exists at target
	MislinkedInternal                  // Symlink exists but points to wrong place in source dir
	MislinkedExternal                  // Symlink exists but points outside source dir
	ExistsIdentical                    // Regular file or dir exists, content matches source
	ExistsModified                     // Regular file or dir exists, content differs from source
)

// Common logic for walking the source directory
// walkSourceDir walks the sourceDir and calls handler for each non-ignored file or directory.
//
// Parameters:
// - sourceDirAbs: the root directory to start walking from. Absolute path.
// - ignoreList: list of filename patterns to skip (e.g., ".git", "*.tmp").
// - handler: callback function called with each file's absolute path, os.FileInfo, and relative path.
//
// The walk skips the root directory itself and any ignored files or folders.
func walkSourceDir(sourceDir string, targetDir string, ignoreList []string, handler func(sourceRel string, info os.FileInfo, state FileState) error) error {

	// Ensure sourceDir is valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got source directory: %s", sourceDir)
	}

	return filepath.Walk(sourceDir, func(sourceRel string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", sourceRel, err)
			return err
		}

		// Skip the root directory (but walk into it)
		isRootDir, err := fileutil.PathsEqual(sourceRel, sourceDir)
		if err != nil {
			return fmt.Errorf("failed to compare paths: %w", err)
		}
		if isRootDir {
			return nil
		}

		// Ignore symlinks in the source directory
		if fileutil.IsSymlink(sourceRel) {
			return nil
		}

		var targetState FileState

		// Ignore any directories or files in the ignore list
		if matched, err := fileutil.MatchesPatterns(info.Name(), ignoreList); err != nil {
			return fmt.Errorf("error checking ignore patterns: %v", err)
		} else if matched {
			targetState = Ignore
		}

		// Get absolute paths
		targetAbs := filepath.Join(targetDir, sourceRel)
		sourceAbs := filepath.Join(sourceRel, sourceRel)

		// Check if the target exists
		if !fileutil.PathExists(targetAbs) {
			targetState = Missing
		} else {
			// Check if the target is a symlink or not
			if fileutil.IsSymlink(targetAbs) {
				// Check if the target is linked to the correct place
				linked, _ := fileutil.IsSymlinkPointingTo(targetAbs, sourceAbs)
				if linked {
					targetState = AlreadyLinked
				} else {
					linkTarget, _ := os.Readlink(targetAbs)
					linkedInSource, _ := fileutil.IsChildPath(linkTarget, sourceDir)
					if linkedInSource {
						targetState = MislinkedInternal
					} else {
						targetState = MislinkedExternal
					}
				}
			} else {
				var bool sameContents
				if fileutil.IsDir(targetAbs) {
					sameContents, _ = fileutil.CompareFileHashes(sourceAbs, targetAbs)
				} else {
					sameContents, _ = fileutil.CompareFileHashes(sourceAbs, targetAbs)
				}
				if sameContents {
					targetState = ExistsIdentical
				} else {
					targetState = ExistsModified
				}
			}
		}

		if err := handler(sourceRel, info, targetState); err != nil {
			return err
		}

		if targetState == Ignore {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
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

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string, shouldIgnore bool) error {

		if shouldIgnore {
			return nil
		}

		targetAbs := filepath.Join(targetDir, sourceRel)

		// Create the symlink if nothing exists there
		if !fileutil.PathExists(targetAbs) {
			sugar.Debugf("No target found, creating link %s", linkString(targetAbs, sourceAbs))
			symlink(sourceAbs, targetAbs, createDirs)
			return nil
		}

		// Track the target file
		targetDest := targetAbs

		// If the target is a symlink, check if it's already correct
		green := color.New(color.FgGreen).SprintFunc()
		if fileutil.IsSymlink(targetAbs) {
			linked, err := fileutil.IsSymlinkPointingTo(targetAbs, sourceAbs)
			if err != nil {
				sugar.Debugf("Failed to verify symlink %s: %v", linkString(targetAbs, sourceAbs), err)
			} else if linked {
				sugar.Infof("%s%s", green("Correct symlink already exists: "), linkString(targetAbs, sourceAbs))
				return nil
			} else {
				linkTarget, err := os.Readlink(targetAbs)
				if err != nil {
					return err
				}
				targetDest, err := filepath.Abs(linkTarget)
				if err != nil {
					return err
				}
				// Dereference the link
				sugar.Debugf("Target %s does not point to source, it points to %s", targetAbs, targetDest)

			}
		}

		// A file is already there, check if it has the same content
		same, _ := fileutil.CompareFileHashes(sourceAbs, targetDest)
		if same {
			sugar.Debugf("Target file has the same content. Creating correct symlink...")
			if err := os.RemoveAll(targetAbs); err != nil {
				return fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
			}
			symlink(sourceAbs, targetAbs, createDirs)
			return nil
		}

		sugar.Debugf("Target file %s has different content than source %s", targetAbs, sourceAbs)
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

		// Create the symlink
		symlink(sourceAbs, targetAbs, createDirs)

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

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string, shouldIgnore bool) error {

		if shouldIgnore {
			return nil
		}

		targetAbs := filepath.Join(targetDir, sourceRel)

		// Skip non-symlink files (we only want symlinks)
		if !fileutil.IsSymlink(targetAbs) {
			return nil
		}

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
		return stats, fmt.Errorf("gatherStats: expected absolute path, got source directory: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return stats, fmt.Errorf("gatherStats: expected absolute path, got target directory: %s", targetDir)
	}

	err := walkSourceDir(sourceDir, ignoreList, func(sourceAbs string, info os.FileInfo, sourceRel string, shouldIgnore bool) error {

		targetAbs := filepath.Join(targetDir, sourceRel)

		if shouldIgnore {
			stats.Ignored++
			return nil
		}

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
		targetDest, _ = fileutil.ExpandPath(targetDest)

		correctSource := targetDest == sourceAbs
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
