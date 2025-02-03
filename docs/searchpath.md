# How railyard finds files

Railyard requires a number of other files besides the railyard executable itself to function:

| Description        | Default Path                              | Output File      |
|--------------------|-------------------------------------------|------------------|
| Configuration File | `$RAILYARD_HOME/railyard.yaml`            | No               |
| Linux Kernel       | `$RAILYARD_HOME/linux/kernel`             | No								|
| Root Filesystem    | `$RAILYARD_HOME/linux/rootfs`             | No								|
| Volumes            | `$RAILYARD_HOME/data/$VOLUME`             | Yes              |
| Docker Socket      | `$RAILYARD_HOME/run/docker.sock`          | Yes              |
| Railyard Socket    | `$RAILYARD_HOME/run/railyard.sock`        | Yes              |
| Log Directory      | `$HOME/Library/Logs/io.github.railyardvm` | Yes              |

By default, a single volume named `docker` holds all docker data. Other volumes can be added by the user by editing `railyard.yaml`.

Note that logs are written to `~/Library/Logs/io.github.railyardvm/railyard.log` by default.

# The RAILYARD_HOME variable
RAILYARD_HOME is a colon separated list of directories that railyard uses by default to find the virtual machine image and configuration files. Railyard
checks each directory in the list in order, and uses the first one that contains the required file. Output files are always created in the 
first directory in the list, even if they exist in later directories.

IF RAILYARDH_HOME is not set, it defaults to ~/Library/Application Support/io.github.railyardvm, followed by the following directories, if they exist:

- ../share/railyard (relative to the railyard executable)
- ~/.local/share/railyard (or $LOCALPKG_PREFIX/share/railyard)
- /opt/homebrew/share/railyard (or $HOMEBREW_PREFIX/share/railyard)
- /usr/local/share/railyard

Note that if the railyard executable or its parent directory are symlinks, ../share/railyard may have multiple permutations, all of which are checked.
