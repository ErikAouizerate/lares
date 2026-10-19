# lares

Run coding agents in a [coi](https://github.com/coipond/coi) sandbox, entered
from any repository.

lares is a small wrapper around coi. It owns one sandbox: a persistent coi
container declared by a profile it installs, entered from any repository because
the repositories are mounted at their host path. On top of that it fixes one
thing coi cannot see: copying from inside the container to the clipboard of the
host session.

```sh
make install          # lares in ~/.local/bin, profile in ~/.coi/profiles/lares
lares build           # build the image the sandbox is made from
lares init            # create the container, once

cd ~/some/repo
lares mount           # mount this repository in the sandbox
lares pi              # pi, in this directory, inside the sandbox
```

## Commands

| command | what it does |
| --- | --- |
| `lares pi [args…]` | pi in the current directory |
| `lares bash [args…]` | a login shell in the current directory |
| `lares herdr [args…]` | herdr in the current directory |
| `lares run [--] <cmd>` | any command in the current directory, as `coi run` does |
| `lares install [--force]` | install the coi profile, from the files embedded in the binary |
| `lares build [--force]` | build the image the sandbox is made from |
| `lares init` | create the container without entering it |
| `lares start` / `stop` | start the container, stop it keeping its state |
| `lares list` | `coi list` |
| `lares mount [<dir>]` | declare a host directory in the profile and mount it now (the current one by default) |
| `lares unmount [<dir>]` | remove a host directory from the profile and unmount it now (the current one by default) |
| `lares uninstall [--purge]` | remove the sandbox, the profile and the binary |

A repository has to be mounted before it can be entered; `lares` says so and
refuses to open a tool in a directory the container cannot see.

## The workspace

coi mounts a workspace at `/workspace` in every container of its own, so the
sandbox has one even though it is entered from the repositories instead. It is
`~/.local/state/lares/workspace`: a neutral directory holding coi's protection
files and pi's session history, which is what `lares uninstall --purge` removes
and what a plain `lares uninstall` keeps.

## The clipboard

A container has no display server, so pi cannot run `wl-copy` or `xclip`: it
copies by asking the terminal to set the clipboard with an OSC 52 sequence,
which travels back to the host with the rest of its output. Terminals built on
VTE — Ptyxis, GNOME Terminal — parse those sequences and drop them, so the copy
goes nowhere.

When lares recognises such a terminal (`VTE_VERSION` is set), it puts itself
between the terminal and coi: it runs the command on a PTY of its own, passes
the output through untouched, and copies the text carried by the OSC 52
sequences to the host clipboard itself. Anywhere else the terminal does the job,
and lares steps aside with `exec` so the command behaves exactly as if the shell
had started it.

The command keeps a real terminal rather than a pipe because incus, underneath
coi, reads the terminal size from its standard output and drops the session on
`SIGWINCH` when there is none to ask.

## Requirements

- `coi` in `PATH`, and `incus` for reading container devices and networks.
- A clipboard tool on the host: `wl-copy` on Wayland, `xclip` or `xsel` on X11.

## Layout

```
cmd/lares          the command
internal/cli       the commands and the coi profile install
internal/coi       calls to coi and incus
internal/ttyproxy  the terminal proxy: PTY, raw mode, resizes
internal/osc52     the OSC 52 filter
internal/clipboard the host clipboard
profile/           the coi profile and its provisioning script, embedded
```
