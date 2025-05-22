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

	"github.com/fatih/color"
	"github.com/spf13/cobra"
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

// LState represents a higher-level state derived from LinkState,
// with awareness of source directories, useful for recursive link operations.
type LState int

const (
	LIgnore            LState = iota // Target should be ignored (e.g., explicitly excluded)
	LAlreadyLinked                   // Correct symlink already exists; no action needed
	LMissing                         // No file/link exists; symlink should be created
	LMislinkedInternal               // Symlink points to the wrong place within the managed source set
	LMislinkedExternal               // Symlink points outside the managed sources; should be corrected
	LExistsIdentical                 // A regular file/dir exists and matches the source; may be replaced with a link
	LExistsModified                  // A regular file/dir exists and differs from the source; replacement may overwrite changes
)

// MapLinkStateToTargetState maps a basic LinkState to an appropriate TargetState.
// More advanced versions can incorporate context like source directories.
func determineTargetState(linkPath, targetPath, targetRoot string, ignoreList []string) (LState, error) {

	sugar.Debugf("Determining link state for: %s", linkString(linkPath, targetPath))

	// Ignore any directories or files in the ignore list
	if matched, err := fileutil.MatchesPatterns(filepath.Base(targetPath), ignoreList); err != nil {
		return LIgnore, fmt.Errorf("error checking ignore patterns: %v", err)
	} else if matched {
		sugar.Debugf("Ignoring target: %s", targetPath)
		return LIgnore, nil
	}

	ls, _ := fileutil.GetLinkState(linkPath, targetPath)

	switch ls {
	case fileutil.AlreadyLinked:
		sugar.Debugf("Link is already in place: %s", linkString(linkPath, targetPath))
		return LAlreadyLinked, nil

	case fileutil.Missing:
		sugar.Debugf("Nothing exists at: %s", linkPath)
		return LMissing, nil

	case fileutil.Mislinked:

		// Read the target
		linkTarget, _ := os.Readlink(linkPath)
		inTarget, _ := fileutil.IsChildPath(linkTarget, targetRoot)
		if inTarget {
			sugar.Debugf("Link is internally mislinked: %s", linkString(linkPath, linkTarget))
			return LMislinkedInternal, nil
		}
		sugar.Debugf("Link is externally mislinked: %s", linkString(linkPath, linkTarget))
		return LMislinkedExternal, nil

	case fileutil.ExistsIdentical:
		sugar.Debugf("File with identical content exists at: %s", linkPath)
		return LExistsIdentical, nil

	// TODO: fix this handling if the link path exists as a dir v.s. file
	case fileutil.ExistsModified:
		sugar.Debugf("File with with different content exists at: %s", linkPath)
		return LExistsModified, nil

	default:
		return LIgnore, nil // Fallback for unknown or unsupported LinkState
	}
}

type handler func(sourceAbs, targetAbs string, targetState LState) (bool, error)

func walkSourceRec(linkRoot, targetRoot string, ignoreList []string, handlerFunc handler) error {

	// Ensure sourceDir is valid
	if !filepath.IsAbs(targetRoot) {
		return fmt.Errorf("walkSourceDir: expected absolute path, got source directory: %s", targetRoot)
	}

	// Since we guarantee targetRoot to be an absolute path, targetPath will also be absolute
	return filepath.Walk(targetRoot, func(targetPath string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error walking directory %s: %v\n", targetPath, err)
			return err
		}

		// Ignore symlinks in the target directory
		if fileutil.IsSymlink(targetPath) {
			return nil
		}

		// Determine the state of the target
		targetRel, _ := filepath.Rel(targetRoot, targetPath) // Source path relative to target dir
		linkPath := filepath.Join(linkRoot, targetRel)       // Absolute path of link path
		linkState, err := determineTargetState(linkPath, targetPath, targetRoot, ignoreList)
		if err != nil {
			return err
		}

		// Handle this element. The handler decides that if this a dir, if we are to skip it
		shouldRecurse, err := handlerFunc(linkPath, targetPath, linkState)
		if err != nil {
			return err
		}
		if (!shouldRecurse || linkState == LIgnore) && info.IsDir() {
			return filepath.SkipDir
		}

		return nil
	})
}

// walkSourceRec recursively walks through the directory tree rooted at targetRoot (must be absolute).
// For each file or directory (excluding symlinks), it determines the corresponding link path under linkRoot,
// checks the link state, and invokes handlerFunc to process it.
//
// Parameters:
//   - linkRoot: the root directory where symlinks will be created or checked.
//   - targetRoot: the root directory to walk through; must be an absolute path.
//   - ignoreList: list of paths or patterns to ignore during the walk.
//   - handlerFunc: a callback function that handles each file or directory and returns whether to recurse further.
//
// The function skips symlinks in targetRoot, respects the ignoreList, and skips directories
// based on the handlerFunc's decision or if the link state is ignored.
func createSymlinks(linkRoot, targetRoot string, force, createDirs, confirm, recursive, fold bool, ignoreList []string) error {

	// Ensure linkPath and targetPath are valid
	if !filepath.IsAbs(linkRoot) {
		return fmt.Errorf("createSymlinks: expected absolute path, got source directory: %s", linkRoot)
	}
	if !filepath.IsAbs(targetRoot) {
		return fmt.Errorf("createSymlinks: expected absolute path, got target directory: %s", targetRoot)
	}

	link := func(linkPath string, targetPath string, createDirs bool) {
		if err := fileutil.CreateSymlink(linkPath, targetPath, createDirs); err != nil {
			sugar.Infof("Error creating symlink %s: %v", linkString(linkPath, targetPath), err)
		} else {
			sugar.Infof("Linked: %s", linkString(linkPath, targetPath))
		}
	}

	handler := func(linkPath, targetPath string, linkState LState) (bool, error) {

		isRoot, _ := fileutil.PathsEqual(targetPath, targetRoot)

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
		if linkState == LIgnore {
			shouldRecurse = false
			return shouldRecurse, nil
		}

		// If not folding on recursive run and this a dir, don't link it!
		if fileutil.IsDir(targetPath) && recursive && !fold {
			return shouldRecurse, nil
		}

		// TODO: factor this out to be more reusable
		switch linkState {
		case LIgnore, LAlreadyLinked:
		case LMissing:
			link(linkPath, targetPath, createDirs)

		case LMislinkedInternal:
			sugar.Debugf("Target file is broken. Creating correct symlink...")
			if err := os.RemoveAll(linkPath); err != nil {
				return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
			}
			link(linkPath, targetPath, createDirs)

		case LMislinkedExternal:
			if force {
				sugar.Infof("Overwriting existing file at: ", linkPath)
				if err := os.RemoveAll(linkPath); err != nil {
					return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
				}
			} else {
				if stringutil.AskForConfirmation("Preview diff of existing file at " + linkPath + "?") {
					PreviewDiff(linkPath, targetPath)
				}
				if stringutil.AskForConfirmation("Delete existing file at " + linkPath + "?") {
					if err := os.RemoveAll(linkPath); err != nil {
						return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
					}
				} else {
					fmt.Printf("Skipped linking: %s\n", linkPath)
				}
			}

		case LExistsIdentical:
			sugar.Debugf("Target file has the same content. Creating correct symlink...")
			if err := os.RemoveAll(linkPath); err != nil {
				return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
			}
			link(linkPath, targetPath, createDirs)

		case LExistsModified:
			if force {
				sugar.Infof("Overwriting existing file at: ", linkPath)
				if err := os.RemoveAll(linkPath); err != nil {
					return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
				}
			} else {
				if stringutil.AskForConfirmation("Preview diff of existing file at " + linkPath + "?") {
					PreviewDiff(linkPath, targetPath)
				}
				if stringutil.AskForConfirmation("Delete existing file at " + linkPath + "?") {
					if err := os.RemoveAll(linkPath); err != nil {
						return shouldRecurse, fmt.Errorf("failed to remove existing file %s: %w", linkPath, err)
					}
				} else {
					fmt.Printf("Skipped: %s\n", linkPath)
				}
			}

		default:
			// Handle unexpected state
		}

		return shouldRecurse, nil
	}

	return walkSourceRec(linkRoot, targetRoot, ignoreList, handler)
}

const ignoreFile = ".lnkitignore"

// Flags
var (
	recursive  bool
	fold       bool
	force      bool
	createDirs bool
)

func main() {

	InitLogger("Debug")

	rootCmd := &cobra.Command{
		Use:   "lnk",
		Short: "Modern symlink manager",
	}

	// Global flags can be defined here if needed

	// link command
	linkCmd := &cobra.Command{
		Use:   "link link_path target_path",
		Short: "Create symlinks from link_path to target_path",
		Args: func(cmd *cobra.Command, args []string) error {
			logArgs()
			if len(args) < 2 {
				return fmt.Errorf("Requires a link_path and target_path")
			}
			return nil
		},
		RunE: runLink,
	}
	linkCmd.Flags().BoolVar(&recursive, "rec", false, "Recursively process nested directories")
	linkCmd.Flags().BoolVar(&fold, "fold", false, "Link whole directories where applicable")
	linkCmd.Flags().BoolVar(&force, "force", false, "Force")
	linkCmd.Flags().BoolVar(&createDirs, "create-dirs", true, "Create dirs")

	rootCmd.AddCommand(linkCmd)
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func logArgs() {
	sugar.Debugf("Flags --------------------")
	sugar.Debugf("Recursive flag: %t ", recursive)
	sugar.Debugf("Recursive flag: %t ", recursive)
	sugar.Debugf("Force flag: %t", force)
	sugar.Debugf("Create dirs flag: %t", createDirs)
	sugar.Debugf("--------------------------")
}

func runLink(cmd *cobra.Command, args []string) error {

	linkPath, err := fileutil.ExpandPath(args[0])
	if err != nil {
		return fmt.Errorf("failed to expand link path: %w", err)
	}

	targetPath, err := fileutil.ExpandPath(args[1])
	if err != nil {
		return fmt.Errorf("failed to expand target path: %w", err)
	}

	sugar.Debugf("linkPath: %s", linkPath)
	sugar.Debugf("TargetPath: %s", targetPath)

	createSymlinks(linkPath, targetPath, force, createDirs, false, recursive, fold, []string{".git"})

	// TODO: Call your existing linking functions
	return nil
}

func runUnlink(cmd *cobra.Command, args []string) error {
	// Your unlink logic here

	// TODO: Call your existing unlinking functions
	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	// Your stats logic here

	// TODO: Call your existing stats functions
	return nil
}
