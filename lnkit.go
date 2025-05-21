package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"lnkit/fileutil"
	"lnkit/stringutil"
	"lnkit/tree"

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
		Ignore:     []string{"lnkit.toml", ".lnkitignore", "*.git"},
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

// TargetState represents a higher-level state derived from LinkState,
// with awareness of source directories, useful for recursive link operations.
type TargetState int

const (
	TargetIgnore            TargetState = iota // Target should be ignored (e.g., explicitly excluded)
	TargetAlreadyLinked                        // Correct symlink already exists; no action needed
	TargetMissing                              // No file/link exists; symlink should be created
	TargetMislinkedInternal                    // Symlink points to the wrong place within the managed source set
	TargetMislinkedExternal                    // Symlink points outside the managed sources; should be corrected
	TargetExistsIdentical                      // A regular file/dir exists and matches the source; may be replaced with a link
	TargetExistsModified                       // A regular file/dir exists and differs from the source; replacement may overwrite changes
)

// MapLinkStateToTargetState maps a basic LinkState to an appropriate TargetState.
// More advanced versions can incorporate context like source directories.
func determineTargetState(sourceDir, targetDir, sourceRel string, ignoreList []string) (TargetState, error) {

	// Get absolute paths
	targetAbs := filepath.Join(targetDir, sourceRel)
	sourceAbs := filepath.Join(sourceDir, sourceRel)

	// Ignore any directories or files in the ignore list
	if matched, err := fileutil.MatchesPatterns(filepath.Base(sourceAbs), ignoreList); err != nil {
		return TargetIgnore, fmt.Errorf("error checking ignore patterns: %v", err)
	} else if matched {
		sugar.Debugf("Ignoring source: %s", sourceAbs)
		return TargetIgnore, nil
	}

	ls, _ := fileutil.GetLinkState(targetAbs, sourceAbs)

	switch ls {
	case fileutil.AlreadyLinked:
		sugar.Debugf("Target link is correct: %s", linkString(targetAbs, sourceAbs))
		return TargetAlreadyLinked, nil

	case fileutil.Missing:
		sugar.Debugf("No target exists: %s", targetAbs)
		return TargetMissing, nil

	case fileutil.Mislinked:

		// Read the target
		linkTarget, _ := os.Readlink(targetAbs)
		inSource, _ := fileutil.IsChildPath(linkTarget, sourceDir)
		if inSource {
			sugar.Debugf("Target link is internally mislinked: %s", linkString(targetAbs, linkTarget))
			return TargetMislinkedInternal, nil
		}
		sugar.Debugf("Target link is externally mislinked: %s", linkString(targetAbs, linkTarget))
		return TargetMislinkedExternal, nil

	case fileutil.ExistsIdentical:
		sugar.Debugf("File exists at target with identical content: %s", targetAbs)
		return TargetExistsIdentical, nil

	case fileutil.ExistsModified:
		sugar.Debugf("File exists at target with different content: %s", targetAbs)
		return TargetExistsModified, nil

	default:
		return TargetIgnore, nil // Fallback for unknown or unsupported LinkState
	}
}

type handler func(sourceAbs, targetAbs string, targetState TargetState) (bool, error)

// Common logic for walking the source directory (for --recursive command calls)
// walkSourceRec walks the sourceDir and calls handler for each non-ignored file or directory.
//
// Parameters:
// - sourceDirAbs: the root directory to start walking from. Absolute path.
// - ignoreList: list of filename patterns to skip (e.g., ".git", "*.tmp").
// - handler: callback function called with each file's absolute path, os.FileInfo, and relative path.
//
// The walk skips the root directory itself and any ignored files or folders.
func walkSourceRec(sourceDir string, targetDir string, ignoreList []string, handlerFunc handler) error {

	// Ensure sourceDir is valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got source directory: %s", sourceDir)
	}

	// Since we guarantee sourDir to be an absolute path, all elements while walking will also be absolute
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

		// Determine the state of the target
		sourceRel, _ := filepath.Rel(sourceDir, sourceAbs) // Source path relative to source dir
		targetAbs := filepath.Join(targetDir, sourceRel)   // Absolute path of target link
		targetState, err := determineTargetState(sourceDir, targetDir, sourceRel, ignoreList)
		if err != nil {
			return err
		}

		// Handle this element. The handler decides that if this a dir, if we are to skip it
		shouldRecurse, err := handlerFunc(sourceAbs, targetAbs, targetState)
		if err != nil {
			return err
		}
		if (!shouldRecurse || targetState == TargetIgnore) && info.IsDir() {
			return filepath.SkipDir
		}

		return nil
	})
}

// Walk the source directory and process symlinks
func createSymlinks(sourceDir, targetDir string, force, createDirs, confirm, recursive, fold bool, ignoreList []string) error {

	// Ensure sourceDir and targetDir are valid
	if !filepath.IsAbs(sourceDir) {
		return fmt.Errorf("createSymlinks: expected absolute path, got source directory: %s", sourceDir)
	}
	if !filepath.IsAbs(targetDir) {
		return fmt.Errorf("createSymlinks: expected absolute path, got target directory: %s", targetDir)
	}

	link := func(sourceAbs string, targetAbs string, createDirs bool) {
		if err := fileutil.CreateSymlink(sourceAbs, targetAbs, createDirs); err != nil {
			sugar.Infof("Error creating symlink %s: %v", linkString(targetAbs, sourceAbs), err)
		} else {
			sugar.Infof("Linked %s", linkString(targetAbs, sourceAbs))
		}
	}

	handler := func(sourceRel, targetAbs string, targetState TargetState) (bool, error) {

		sourceAbs := filepath.Join(sourceDir, sourceRel)
		isRoot, _ := fileutil.PathsEqual(sourceAbs, sourceDir)

		// If performing a recursive link, allow walking into subdirectories.
		// Otherwise, skip walking deeper after processing the current item.
		// This means:
		// - For files: no recursion occurs regardless, so behavior is unaffected.
		// - For directories:
		//   - Non-recursive: we process the directory itself, but do not descend.
		//   - Recursive: we process and descend into subdirectories.
		//
		// Effectively, this controls whether we recurse beyond the root directory.
		shouldRecurse := false // Whether we should recurse into the dir
		if recursive && (isRoot || !fold) {
			shouldRecurse = true
		}

		// Skip and don't recurse into ignored elements
		if targetState == TargetIgnore {
			shouldRecurse = false
			return shouldRecurse, nil
		}

		// If not folding on recursive run and this a dir, don't link it!
		if fileutil.IsDir(sourceAbs) && recursive && !fold {
			return shouldRecurse, nil
		}

		// TODO: factor this out to be more reusable
		switch targetState {
		case TargetIgnore, TargetAlreadyLinked:
		case TargetMissing:
			link(sourceAbs, targetAbs, createDirs)
			return shouldRecurse, nil
		case TargetMislinkedInternal:
			sugar.Debugf("Target file is broken. Creating correct symlink...")
			if err := os.RemoveAll(targetAbs); err != nil {
				return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
			}
			link(sourceAbs, targetAbs, createDirs)
			return shouldRecurse, nil

		case TargetMislinkedExternal:
			if force {
				sugar.Infof("Overwriting existing file at: ", targetAbs)
				if err := os.RemoveAll(targetAbs); err != nil {
					return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
				}
			} else {
				if stringutil.AskForConfirmation("Preview diff of existing file at " + targetAbs + "?") {
					PreviewDiff(sourceAbs, targetAbs)
				}
				if stringutil.AskForConfirmation("Delete existing file at " + targetAbs + "?") {
					if err := os.RemoveAll(targetAbs); err != nil {
						return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
					}
				} else {
					fmt.Printf("Skipped: %s\n", targetAbs)
					return shouldRecurse, nil
				}
			}

		case TargetExistsIdentical:
			sugar.Debugf("Target file has the same content. Creating correct symlink...")
			if err := os.RemoveAll(targetAbs); err != nil {
				return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
			}
			link(sourceAbs, targetAbs, createDirs)
			return shouldRecurse, nil

		case TargetExistsModified:
			if force {
				sugar.Infof("Overwriting existing file at: ", targetAbs)
				if err := os.RemoveAll(targetAbs); err != nil {
					return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
				}
			} else {
				if stringutil.AskForConfirmation("Preview diff of existing file at " + targetAbs + "?") {
					PreviewDiff(sourceAbs, targetAbs)
				}
				if stringutil.AskForConfirmation("Delete existing file at " + targetAbs + "?") {
					if err := os.RemoveAll(targetAbs); err != nil {
						return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", targetAbs, err)
					}
				} else {
					fmt.Printf("Skipped: %s\n", targetAbs)
					return shouldRecurse, nil
				}
			}

		default:
			// Handle unexpected state
		}

		return shouldRecurse, nil
	}

	return walkSourceRec(sourceDir, targetDir, ignoreList, handler)
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

	unlink := func(sourceAbs, targetAbs string) {
		if err := fileutil.RemoveSymlink(targetAbs); err != nil {
			sugar.Infof("Error removing symlink %s: %v", linkString(targetAbs, sourceAbs), err)
		} else {
			sugar.Infof("Removed symlink: %s", linkString(targetAbs, sourceAbs))
		}
	}

	handler := func(sourceRel, targetAbs string, targetState TargetState) (bool, error) {

		// TODO: factor this out to be more reusable
		sourceAbs := filepath.Join(sourceDir, sourceRel)

		switch targetState {
		case TargetIgnore, TargetMissing, TargetExistsIdentical, TargetExistsModified:
		case TargetAlreadyLinked, TargetMislinkedInternal:
			unlink(sourceAbs, targetAbs)
		case TargetMislinkedExternal:

			// Ask for confirmation if needed
			if confirm && !stringutil.AskForConfirmation(fmt.Sprintf("Remove symlink %s?", linkString(targetAbs, sourceAbs))) {
				return false, nil
			}

			unlink(sourceAbs, targetAbs)

		default:
			// Handle unexpected state
		}

		return false, nil
	}

	return walkSourceRec(sourceDir, targetDir, ignoreList, handler)
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

	err := walkSourceRec(sourceDir, targetDir, ignoreList, func(sourceRel, targetAbs string, targetState TargetState) (bool, error) {

		switch targetState {
		case TargetIgnore:
			stats.Ignored++
			return true, nil // If the target is a directory, don't go into it
		case TargetMissing:
			stats.NoTarget++
			stats.Unlinked++
		case TargetAlreadyLinked:
			stats.LinkedDirs++
			stats.LinkedFiles++
		case TargetMislinkedInternal:
			stats.IncorrectSymlink++
		case TargetMislinkedExternal:
			stats.IncorrectSymlink++
		case TargetExistsIdentical:
			stats.SameContents++
		case TargetExistsModified:
			stats.DifferentContents++
		default:
			// Handle unexpected state
		}

		return true, nil

	})

	return stats, err
}

func printStats(sourceDir string, targetDir string, ignore []string) {
	stats, err := gatherStats(sourceDir, targetDir, ignore)
	if err != nil {
		sugar.Fatalf("Error gathering stats: %v", err)
	}

	root := &tree.TreeNode{Text: "Stats"}

	linked := &tree.TreeNode{Text: fmt.Sprintf("Linked files (%d)", stats.LinkedFiles), Icon: "✔", Color: color.New(color.FgGreen).SprintFunc()}
	ignored := &tree.TreeNode{Text: fmt.Sprintf("Ignored files (%d)", stats.Ignored), Icon: "―", Color: color.New(color.FgBlue).SprintFunc()}

	unlinked := &tree.TreeNode{Text: fmt.Sprintf("Unlinked files (%d)", stats.Unlinked), Icon: "✖", Color: color.New(color.FgRed).SprintFunc()}
	unlinked.Children = []*tree.TreeNode{
		{Text: "Target does not exist", Icon: "•", Color: color.New(color.FgRed).SprintFunc()},
		{Text: "Broken symlink", Icon: "•", Color: color.New(color.FgRed).SprintFunc()},
		{Text: "Same content, not linked", Icon: "•", Color: color.New(color.FgRed).SprintFunc()},
	}

	root.Children = []*tree.TreeNode{linked, unlinked, ignored}

	tree.PrintTreeNode(root, "", true)
}

const ignoreFile = ".lnkitignore"

type Args struct {
	Command    string `arg:"positional,required" help:"command to run (link, unstow, stats)"`
	ConfigFile string `arg:"-c,--config" help:"path to config file" default:"lnkit.toml"`
	TargetDir  string `arg:"-t,--target" help:"Override target directory"`
	SourceDir  string `arg:"-s,--source" help:"Override source directory"`
	Recursive  bool   `arg:"--rec" help:"Recursively process nested directories"`
	Fold       bool   `arg:"--fold" help:"Link whole directories where applicable"`
}

var recursive = false
var fold = false
var create_dirs = true

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
		if err := createSymlinks(sourceDir, targetDir, cfg.Options.Force, cfg.Options.CreateDirs, cfg.Options.Confirm, args.Recursive, args.Fold, cfg.Options.Ignore); err != nil {
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
