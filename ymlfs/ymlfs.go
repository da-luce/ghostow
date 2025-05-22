package ymlfs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

// FromYml parses YAML data describing a directory structure with files, directories, and symlinks,
// and creates the corresponding structure on disk rooted at rootDir.
func FromYml(rootDir string, yamlData []byte) error {
	var root map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &root); err != nil {
		return err
	}
	return createStructure(rootDir, root)
}

func createStructure(base string, node map[string]interface{}) error {
	for name, val := range node {
		switch typed := val.(type) {
		case map[string]interface{}:
			typ, _ := typed["type"].(string)

			switch typ {
			case "file":
				path := filepath.Join(base, name)
				content, ok := typed["content"].(string)
				if !ok {
					return fmt.Errorf("file %s missing 'content'", name)
				}
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return err
				}
				if _, err := f.WriteString(content); err != nil {
					f.Close()
					return err
				}
				f.Close()

			case "symlink":
				target, ok := typed["target"].(string)
				if !ok {
					return fmt.Errorf("symlink %s missing 'target'", name)
				}
				linkPath := filepath.Join(base, name)
				if err := os.Symlink(target, linkPath); err != nil {
					return err
				}

			case "":
				// No "type" key → treat as directory
				dirPath := filepath.Join(base, name)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					return err
				}
				if err := createStructure(dirPath, typed); err != nil {
					return err
				}

			default:
				return fmt.Errorf("unsupported type %q for %s", typ, name)
			}

		case nil:
			// nil means empty directory
			dirPath := filepath.Join(base, name)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported value type for %s: %T", name, val)
		}
	}
	return nil
}

// ToYml reads the directory structure and files at rootDir and returns
// a YAML representation of the structure including symlinks and directories.
func ToYml(rootDir string) ([]byte, error) {
	info, err := os.Stat(rootDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("rootDir must be a directory")
	}

	tree, err := buildYmlTree(rootDir)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(tree)
}
func buildYmlTree(base string) (map[string]interface{}, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(base, name)

		info, err := entry.Info()
		if err != nil {
			return nil, err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return nil, err
			}

			// Convert absolute target to relative to base, if possible
			if filepath.IsAbs(target) {
				if rel, err := filepath.Rel(base, target); err == nil {
					target = rel
				}
			}

			result[name] = map[string]interface{}{
				"type":   "symlink",
				"target": target,
			}

		} else if info.IsDir() {
			// Directory: recurse, no "type: dir" key
			subtree, err := buildYmlTree(path)
			if err != nil {
				return nil, err
			}

			if len(subtree) == 0 {
				// empty dir represented as nil
				result[name] = nil
			} else {
				result[name] = subtree
			}

		} else {
			// File: must have content and type:file
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}

			result[name] = map[string]interface{}{
				"type":    "file",
				"content": string(content),
			}
		}
	}

	return result, nil
}

// ToMap unmarshals YAML bytes into a map[string]interface{}.
func ToMap(data []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	err := yaml.Unmarshal(data, &m)
	return m, err
}

// AssertStructure compares the actual filesystem at dirPath against the expected YAML structure.
// Returns (true, nil) if they match, (false, nil) if they don't match, or (false, err) if an error occurs.
func AssertStructure(dirPath string, expectedYaml string) (bool, error) {
	actualYaml, err := ToYml(dirPath)
	if err != nil {
		return false, fmt.Errorf("failed to generate YAML from directory: %w", err)
	}

	actualMap, err := ToMap(actualYaml)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal actual YAML: %w", err)
	}

	expectedMap, err := ToMap([]byte(expectedYaml))
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal expected YAML: %w", err)
	}

	if !reflect.DeepEqual(expectedMap, actualMap) {
		diff := cmp.Diff(expectedMap, actualMap)
		return false, fmt.Errorf("structure mismatch:\n%s", diff)
	}

	return true, nil
}
