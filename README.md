<h1>
  lnkit
  <img src="./media/toolbox.png" alt="Description" height="40" style="vertical-center: bottom;" />
</h1>

`lnkit` is a modern, cross-platform tool box for managing symlinks, built in Go. It simplifies the creation, maintenance, and organization of symbolic links, making it easier to manage complex file systems. Designed for speed and reliability, `lnkit` works seamlessly across different operating systems to streamline your workflow.

- Source Directory: This is the directory where your files (such as configuration files, scripts, etc.) reside. A `lnkit.toml` file must be present to document the fact that `lnkit` is managing this directory.
- Target Directory: This is where the symlinks will point to. The files in the target directory are what the symlinks in the source directory will reference.

| **Command**                                                                                                         | **Description**                                                                                                                                                                               | **Status** |
| ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| `lnk link [-vnfr] source [target]`                                                                                  | Creates symlink at target pointing back to the source                                                                                                                                         | ✅*        |
| `lnk unlink [-vnfr] [target=.]`                                                                                     | Removes target symlink                                                                                                                                                                        | ✅         |
| `lnk relativize [-vnfr] [target=.]`                                                                                 | Convert absolute symlink to relative                                                                                                                                                          | ❌         |
| `lnk list [-vnr] [target=.]`                                                                                        | Lists all symlinks inside the target directory                                                                                                                                                | ❌         |
| `lnk clean [-vnfr] [target=.]`                                                                                      | Remove broken symlinks inside target                                                                                                                                                          | ❌         |
| `lnk scan [-vn] [target=.] [--max-depth=n]`                                                                         | Lists all symlinks in target including the depth of each symlink.                                                                                                                             | ❌         |
| `lnk track [-vnfr] symlink [--pattern=pattern \| --sort=version\|time\|name \| --script=path] [--item=first\|last]` | Creates or updates a tracking symlink based on matching targets filtered by pattern, sorted by criteria, or dynamically resolved by a user script. Selects first or last match.               | ❌*        |
| lnk autolink [-vnfr] [--pattern=pattern] [--target=dir=~] [--folders ...]                                           | Automatically scans the specified directory (e.g., a shared volume) for matching folders and creates symlinks in the target directory, maintaining or fixing links for selected folder names. | ❌*        |

### `link --recursive`

The `--recursive` option is especially useful when managing dotfiles or configuration directories. Instead of linking an entire directory as a single symlink (e.g., ~/.config/nvim → ~/dotfiles/nvim), recursive linking creates symlinks for each individual file and folder inside the source directory directly into the target (e.g., linking all files inside ~/dotfiles/nvim/ into ~/.config/). This approach mirrors how tools like [GNU Stow](https://www.gnu.org/software/stow/) handle dotfiles, allowing you to manage and update configuration files granularly without nesting entire directories, making your dotfiles setup more modular, transparent, and easier to maintain.

### `track`

`lnk track` enables creating and maintaining dynamic symlinks that automatically update to point to the most relevant target based on a specified pattern and sorting criteria. For example, in an Obsidian vault, you might have a symlink `/journals/today` that always points to the daily note for the current date, making it easy to quickly access or open today’s journal entry. Similarly, for software development, a symlink like `/path/SDK/current` can be tracked to always point to the latest installed SDK version folder (e.g., SDK-1.9, SDK-1.10, SDK-2.0), saving you the hassle of manually updating the link whenever a new SDK version is installed. This feature supports flexible pattern matching, sorting (by version, time, or name), and even custom scripts to resolve targets, making it a powerful tool for managing "moving" symlinks that reflect evolving environments.

### `lnk autolink`

The autolink command automates creating and maintaining symlinks for commonly shared large folders—such as Downloads, Videos, or Documents—that reside on separate storage volumes shared across multiple Linux distributions. Instead of manually creating or updating these links for each environment, lnk autolink scans a specified source directory for folders matching a user-defined pattern or name list and ensures corresponding symlinks exist in the target directory (like the home folder). This approach is especially valuable for users running multi-distro setups (e.g., with NixOS and others) who want to avoid the overhead and complexity of rebuilding system configurations just to update symlinks. By decoupling symlink management from system tooling, lnk autolink offers a fast, distro-agnostic, and convenient solution to keep user environments consistent and up to date.

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
  - [ ] `/path/SDK/current` should be updated to always point to the most up to date SDK... use lua? [Example to implement using inotify](https://unix.stackexchange.com/questions/573949/can-i-setup-a-symlink-to-the-most-recent-folder)

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
