# ymlfs: Yaml File System

Tooling that interacts directly with the filesystem should be tested intensively to ensure it behaves exactly as intended—we don’t want to accidentally delete files or corrupt data. So, I made `ymlfs`: a Go package that converts between YAML representations of filesystem structures and actual directories on disk, making testing easier. It supports regular files, directories, and symlinks.

FIXME: **I haven't come across a standardized way to represent file trees as serialized data, so until I find one, this sacrilegious package exists purely out of necessity.**

## Features

- 🗂️ Define directory trees in YAML
- 🧾 Create those trees on disk using `FromYml`
- 🔁 Serialize a filesystem structure back to YAML using `ToYml`
- 🔗 Supports symlinks with relative paths

## Usage

### YAML Format

Any key with nested children is considered a directory by default:

```yml
script.sh: {type: file, content: "echo Hello"}
mydir:
  readme.md: {type: file, content: "Some nice text here..."}
  notes.txt: {type: file, content: "Some notes here."}
shortcut: {type: symlink, target: script.sh}
```

This YAML corresponds to the following directory structure:

```text
.
├── file.txt
├── config/
├── .dotfiles/
│   ├── file2.txt
│   └── dirB/
│       └── file3.txt
└── link.txt -> file.txt
```

### Methods

- `ymlfs.FromYml("/path/to/output", yamlData)`: creates the directory structure and files at the specified path based on the given YAML data.
- `ymlfs.ToYml("/path/to/input")`: reads the directory structure and files at the specified path and returns the corresponding YAML representation.
- `ymlfs.AssertStructure("/path/to/comapre", expectedYamlStructure)`: compares the actual filesystem at the given path against the expected YAML structure and returns whether they match (optionally with detailed mismatch info).
