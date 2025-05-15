# gostow

gostow is a modern alternative to [GNU Stow](https://www.gnu.org/software/stow/), built in Go, with additional features.

Much like with stow, there are two primary directories involved:

* Source Directory: This is the directory where your files (such as configuration files, scripts, etc.) reside. A `gostow.toml` file must be present to document the fact that Gostow is managing this directory.
* Target Directory: This is where the symlinks will point to. The files in the target directory are what the symlinks in the source directory will reference.

| **Command** | **Description**                                                        |
| ----------- | ---------------------------------------------------------------------- |
| `link`      | Creates symlinks in the target directory, pointing back to the source. |
| `unlink`    | Removes symlinks in the target directory.                              |
| `stats`     | Displays statistics about the current symlinks and their statuses.     |

Here are the default settings for a `gostow.toml` configuration file:

```toml
[options]
confirm = false     # Disable confirmation prompt in the CLI (set to true for confirmation before actions).
force = false       # If set to true, existing files in the target directory will be overwritten without prompt.
create_dirs = true  # If set to true, automatically create any missing directories in the target path.
source_dir = "."    # Path to the source directory containing the files to be linked.
target_dir = "~"    # Path to the target directory where symlinks will be created.

# Custom links allow you to specify exceptions or modify the structure of the source directory.
[exceptions]
```
