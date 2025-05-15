package fileutil

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
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

func ExpandPath(path string) string {
	// Expands the ~ to the full home directory path
	if strings.HasPrefix(path, "~") {
		usr, _ := user.Current()
		return filepath.Join(usr.HomeDir, path[1:])
	}
	return os.ExpandEnv(path)
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

// Create a symlink at the target location
func CreateSymlink(source, dest string, createDirs bool) error {
	// Ensure the target directory exists
	dir := filepath.Dir(dest)
	if createDirs {
		if !IsDir(dir) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}

	// Create the symlink
	if err := os.Symlink(source, dest); err != nil {
		return fmt.Errorf("failed to create symlink from %s to %s: %w", source, dest, err)
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
