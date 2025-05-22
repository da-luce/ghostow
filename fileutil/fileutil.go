package fileutil

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// PathExists returns true if the given path exists (file, dir, symlink, etc.).
func PathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// IsRegularFile returns true if the path is a regular file (not a dir or symlink).
func IsRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsSymlink returns true if the path is a symlink (regardless of target).
func IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && (info.Mode()&os.ModeSymlink != 0)
}

// DirExists returns true if the given path is an existing directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsChildPath returns true if `a` is a child of `b`.
// a: the potential child path
// b: the potential parent path
// TODO: this is hacky?
func IsChildPath(a, b string) (bool, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}

	absB, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(absB, absA)
	if err != nil {
		return false, err
	}

	if rel == "." || strings.HasPrefix(rel, "..") {
		return false, nil
	}
	return true, nil
}

// hashFile generates a SHA-256 hash for the given file.
func HashFile(path string) ([]byte, error) {
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
func CompareFileHashes(file1, file2 string) (bool, error) {
	hash1, err := HashFile(file1)
	if err != nil {
		return false, err
	}

	hash2, err := HashFile(file2)
	if err != nil {
		return false, err
	}

	// Compare the hashes
	return bytes.Equal(hash1, hash2), nil
}

// CompareDirs compares the contents of two directories by relative paths and file content.
// It returns a list of differences and an error if one occurred during comparison.
func CompareDirHashes(dir1, dir2 string) ([]string, error) {
	var diffs []string

	// Walk dir1 and compare each file to its counterpart in dir2
	err := filepath.WalkDir(dir1, func(path1 string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir1, path1)
		if err != nil {
			return err
		}

		path2 := filepath.Join(dir2, relPath)

		// Check if file exists in dir2
		info2, err := os.Stat(path2)
		if os.IsNotExist(err) {
			diffs = append(diffs, fmt.Sprintf("Missing in dir2: %s", relPath))
			return nil
		} else if err != nil {
			return err
		}

		// Make sure it's a file
		if info2.IsDir() {
			diffs = append(diffs, fmt.Sprintf("Type mismatch (dir in dir2): %s", relPath))
			return nil
		}

		// Compare file contents
		same, err := CompareFileHashes(path1, path2)
		if err != nil {
			return err
		}
		if !same {
			diffs = append(diffs, fmt.Sprintf("Contents differ: %s", relPath))
		}

		return nil
	})
	if err != nil {
		return diffs, err
	}

	// Walk dir2 to find files not in dir1
	err = filepath.WalkDir(dir2, func(path2 string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir2, path2)
		if err != nil {
			return err
		}

		path1 := filepath.Join(dir1, relPath)
		if _, err := os.Stat(path1); os.IsNotExist(err) {
			diffs = append(diffs, fmt.Sprintf("Extra in dir2: %s", relPath))
		}

		return nil
	})
	if err != nil {
		return diffs, err
	}

	return diffs, nil
}

// MatchesAnyPattern checks if `value` matches any of the patterns in the list.
// Returns true if matched, or error if any pattern is invalid.
func MatchesPatterns(value string, patterns []string) (bool, error) {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, value)
		if err != nil {
			return false, fmt.Errorf("error matching pattern %q: %w", pattern, err)
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func ExpandPath(path string) (string, error) {
	// Expand ~ to the home directory
	if strings.HasPrefix(path, "~") {
		usr, err := user.Current()
		if err != nil {
			return "", err
		}
		path = filepath.Join(usr.HomeDir, strings.TrimPrefix(path, "~"))
	}

	// Expand environment variables
	path = os.ExpandEnv(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return absPath, nil
}

func ReadFileLines(filePath string, ignoreBlank bool) ([]string, error) {
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

// Create a symlink
// CreateSymlink creates a symlink at linkPath pointing to targetPath.
// If createDirs is true, it ensures the parent directory of linkPath exists.
// It returns an error if the symlink already exists or the path is taken.
func CreateSymlink(linkPath, targetPath string, createDirs bool) error {
	if createDirs {
		parent := filepath.Dir(linkPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", linkPath, err)
		}
	}

	// Check if the link path already exists
	if _, err := os.Lstat(linkPath); err == nil {
		return fmt.Errorf("path already exists at %s", linkPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error checking if path exists: %w", err)
	}

	// Create the symlink
	if err := os.Symlink(targetPath, linkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// RemoveSymlink deletes a symlink at the given path if it exists and is a symlink.
func RemoveSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to do
		}
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("path %s is not a symlink", path)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove symlink %s: %w", path, err)
	}

	return nil
}

// IsSymlinkPointingTo returns true if `path` is a symlink that points to `target`.
// It resolves relative symlink targets to absolute paths for accurate comparison.
func IsSymlinkPointingTo(symlink, target string) (bool, error) {
	linkTarget, err := os.Readlink(symlink)
	if err != nil {
		return false, err
	}

	linkTargetAbs, err := filepath.Abs(linkTarget)
	if err != nil {
		return false, err
	}

	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}

	return linkTargetAbs == targetAbs, nil
}

func PathsEqual(a, b string) (bool, error) {
	ra, err := filepath.EvalSymlinks(filepath.Clean(a))
	if err != nil {
		return false, err
	}
	rb, err := filepath.EvalSymlinks(filepath.Clean(b))
	if err != nil {
		return false, err
	}
	return ra == rb, nil
}

// LinkState represents the state of a target with respect to the intended symlink
type LinkState int

const (
	AlreadyLinked   LinkState = iota // Correct symlink exists
	Missing                          // No file or link exists at target
	Mislinked                        // Symlink exists but points to wrong place
	ExistsIdentical                  // Regular file or dir exists, content matches source
	ExistsModified                   // Regular file or dir exists, content differs from source
)

// Determine the state of a symlink linking target to source (target ~> source)
func GetLinkState(targetAbs, sourceAbs string) (LinkState, error) {

	if !filepath.IsAbs(sourceAbs) {
		return Missing, fmt.Errorf("sourceAbs: expected absolute path, got: %s", sourceAbs)
	}
	if !filepath.IsAbs(targetAbs) {
		return Missing, fmt.Errorf("targetAbs: expected absolute path, got: %s", targetAbs)
	}

	// Target path doesn't exist
	if !PathExists(targetAbs) {
		return Missing, nil
	}

	// Target is a symlink
	if IsSymlink(targetAbs) {
		linked, _ := IsSymlinkPointingTo(targetAbs, sourceAbs)
		if linked {
			return AlreadyLinked, nil
		} else {
			return Mislinked, nil
		}
	}

	// Not a symlink—check file or dir content
	// FIXME: does this work with dirs?
	same, _ := CompareFileHashes(sourceAbs, targetAbs)
	if same {
		return ExistsIdentical, nil
	}

	return ExistsModified, nil
}
