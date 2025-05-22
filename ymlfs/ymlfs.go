package ymlfs

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FromYml parses YAML data describing a directory structure with symlinks and
// creates the corresponding structure on disk rooted at rootDir.
func FromYml(rootDir string, yamlData []byte) error {
	var root map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &root); err != nil {
		return err
	}
	return createStructure(rootDir, root)
}

func createStructure(base string, node map[string]interface{}) error {
	for name, val := range node {
		switch v := val.(type) {
		case nil:
			// Create empty file
			fpath := filepath.Join(base, name)
			f, err := os.Create(fpath)
			if err != nil {
				return err
			}
			f.Close()

		case map[string]interface{}:
			if target, ok := v["symlink"]; ok {
				targetStr, ok := target.(string)
				if !ok {
					return fmt.Errorf("symlink target for %s is not a string", name)
				}
				linkPath := filepath.Join(base, name)
				err := os.Symlink(targetStr, linkPath)
				if err != nil {
					return err
				}
			} else {
				// Directory
				dirPath := filepath.Join(base, name)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					return err
				}
				if err := createStructure(dirPath, v); err != nil {
					return err
				}
			}

		default:
			return fmt.Errorf("unsupported value type for %s: %T", name, val)
		}
	}
	return nil
}

// ToYml walks a directory tree rooted at rootDir and returns a YAML
// representation of the structure in the format:
// - files: null
// - directories: nested maps
// - symlinks: map with "symlink" key and target string
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

			// Convert absolute target to relative to root, if possible
			if filepath.IsAbs(target) {
				rel, err := filepath.Rel(base, target)
				if err == nil {
					target = rel
				}
				// else keep target as is if Rel fails
			}

			result[name] = map[string]interface{}{"symlink": target}
		} else if info.IsDir() {
			subtree, err := buildYmlTree(path)
			if err != nil {
				return nil, err
			}
			result[name] = subtree
		} else {
			// regular file
			result[name] = nil
		}
	}

	return result, nil
}

// toMap unmarshals YAML bytes into a map[string]interface{}.
func ToMap(data []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	err := yaml.Unmarshal(data, &m)
	return m, err
}
