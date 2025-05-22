# ymlfs

`ymlfs` is a Go package for converting between YAML representations of filesystem structures and actual directories on disk. It supports regular files, directories, and symlinks.

## Features

- 🗂️ Define directory trees in YAML
- 🧾 Create those trees on disk using `FromYml`
- 🔁 Serialize a filesystem structure back to YAML using `ToYml`
- 🔗 Supports symlinks with relative paths

## Usage

### YAML Format

```yml
file.txt: null
config: {}
.dotfiles:
  file2.txt: null
  dirB:
    file3.txt: null
link.txt:
  symlink: file.txt
```

corresponds to

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

### Create structure from YAML

```go
import (
    "os"
    "github.com/yourusername/yourrepo/ymlfs"
)

func main() {
    yamlData := []byte(`
file.txt: null
mydir: {}
link.txt:
  symlink: file.txt
`)

    err := ymlfs.FromYml("/path/to/output", yamlData)
    if err != nil {
        panic(err)
    }
}
```

Convert existing directory to YAML

```go
outYaml, err := ymlfs.ToYml("/path/to/dir")
if err != nil {
    panic(err)
}
os.Stdout.Write(outYaml)
```