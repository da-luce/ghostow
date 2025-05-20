<h1>
  lnkit
  <img src="./media/toolbox.png" alt="Description" height="40" style="vertical-center: bottom;" />
</h1>


`lnkit` is a modern, cross-platform tool box for managing symlinks, built in Go. It simplifies the creation, maintenance, and organization of symbolic links, making it easier to manage complex file systems. Designed for speed and reliability, `lnkit` works seamlessly across different operating systems to streamline your workflow.

- Source Directory: This is the directory where your files (such as configuration files, scripts, etc.) reside. A `lnkit.toml` file must be present to document the fact that `lnkit` is managing this directory.
- Target Directory: This is where the symlinks will point to. The files in the target directory are what the symlinks in the source directory will reference.

| **Command**        | **Description**                                                        | **Implementation** |
| ------------------ | ---------------------------------------------------------------------- | ------------------ |
| `lnk link`       | Creates symlinks in the target directory, pointing back to the source. | ✓                 |
| `lnk unlink`     | Removes symlinks in the target directory.                              | ✓                 |
| `lnk stats`      | Displays statistics about the current symlinks and their statuses.     | ✓                 |
| `lnk clean`      | Removes broken or orphaned symlinks from the target directory.         | ✕                 |
| `lnk relativize` | Convert absolute symlink to relative                                   | ✕                 |

Here are the default settings for a `lnkit.toml` configuration file:

```toml
[options]
confirm = false     # Disable confirmation prompt in the CLI (set to true for confirmation before actions).
force = false       # If set to true, existing files in the target directory will be overwritten without prompt.
create_dirs = true  # If set to true, automatically create any missing directories in the target path.
source_dir = "."    # Path to the source directory containing the files to be linked.
target_dir = "~"    # Path to the target directory where symlinks will be created.
log_level = "info"  # Log verbosity level. Options: "debug", "info", "warn", "error", "dpanic", "panic", "fatal"
```

## TODO

- [ ] `--dry-run` option
- [ ] Better CLI interface (e.g. y/N/p for previewing a different file)
- [ ] Add other general [symlink utilities](https://github.com/brandt/symlinks)

## Sources

- [Tool Image by Freepik](https://www.freepik.com/icon/tool-box_15996443)

## Why not use a bare Git repo for dotfiles?

- I have gotten into the (perhaps reckless) habit of running `add .` and `git push` all the time—this makes managing a bare repo in `~` a bit annoying (or dangerous!)
- Bare repos clutter `~` with Git metadata or force all dotfiles into home
- To add a root level `README.md` to your dotfiles, you have to have a `README.md` always present in your home directory
- Keeping templates and generated files separate keeps your home clean and organized

## Why not a more complicated dotfiles solution?

- Tools should do one thing, and do that thing well. Akin to the first tenet in the [Unix philosphy](https://en.wikipedia.org/wiki/Unix_philosophy)
- Want templating? Use Jinja2 (or a similar tool) as a separate step before linking your dotfiles,
- The best setups empower _learning_ and creativity—they don’t force you into rigid workflows or obscure magic
