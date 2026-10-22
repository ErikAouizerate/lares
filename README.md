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
lares pi              # pi, in this directory, inside the sandbox
```

`make install` creates `~/.coi/profiles/lares/secrets.env` with empty values.
Fill it with the credentials the sandbox should use before the first
`lares pi`; see [Secrets](#secrets).

## Commands

| command | what it does |
| --- | --- |
| `lares pi [args…]` | pi in the current directory (a shortcut for entering and running pi) |
| `lares bash [args…]` | a login shell in the current directory, prepared like `lares pi` |
| `lares herdr [args…]` | herdr in the current directory |
| `lares run [--] <cmd>` | any command in the current directory, in the same prepared sandbox |
| `lares install [--force]` | install the coi profile, from the files embedded in the binary |
| `lares build [--force]` | build the image the sandbox is made from |
| `lares init` | create the container without entering it |
| `lares start` / `stop` | start the container, stop it keeping its state |
| `lares list` | `coi list` |
| `lares mount [<dir>]` | declare a host directory in the profile and mount it now (the current one by default) |
| `lares unmount [<dir>]` | remove a host directory from the profile and unmount it now (the current one by default) |
| `lares uninstall [--purge]` | remove the sandbox, the profile and the binary |

A repository is mounted when lares is run from it: a directory the container
cannot see is declared in the profile and mounted on the spot, so entering one
needs no separate command and it comes back with the next container.

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

## Secrets

`~/.coi/profiles/lares/secrets.env` is the single place holding the sandbox's
credentials. It is shell syntax (`KEY=VALUE` lines) and mode 0600. `lares
install` creates it with empty values; fill it with credentials dedicated to
the sandbox, not the host ones. Anything in it is copied into the sandbox, so
keep host-only secrets out of it.

When lares starts a tool, it copies the file to `~/.config/lares/secrets`
(0600) and pushes a small `/usr/local/bin/lares-token` helper into the
container. `pi` and `herdr` additionally get the rendered files below, which
call the helper instead of carrying the values:

| file | what it resolves |
| --- | --- |
| `~/.pi/agent/auth.json` | every `api_key` provider |
| `~/.pi/agent/mcp-adapter.json` | the tokens named in its `Bearer ${VAR}` headers |
| `~/.pi/agent/web-search.json` | the Tavily API key |

`lares-token VARIABLE` prints one value from the file; there is no list of
known names to keep in sync. An MCP `Bearer ${CONTEXT7_KEY}` header is resolved
from `CONTEXT7_KEY` directly, and every `api_key` provider in `auth.json` is
resolved from the provider name uppercased with `-` replaced by `_`
(`opencode-go` → `OPENCODE_GO`). OAuth entries are left alone.

The tokens never sit in the container environment, and the shell commands pi
runs do not inherit them. They are read from the file when pi needs them, so a
rotation is picked up the next time the sandbox is entered, without rebuilding
anything. The `opencode-go` key comes from `OPENCODE_GO` in `secrets.env`, not
from the host `auth.json`: the sandbox can use a different provider key.

Every entry prepares the sandbox the same way: `lares bash` gets the secrets,
the helper and pi's rendered configuration, so running `pi` or `herdr` inside it
works just like `lares pi`. `lares pi` is only a shortcut for entering and
running pi.

`lares uninstall` removes the profile but keeps `secrets.env`, so a reinstall
starts from the same sandbox credentials.

This centralizes the secrets and avoids accidental leaks (environment dumps,
child processes, logs). It is **not** a security boundary: the sandbox user has
passwordless sudo, so an agent that decides to read the file can. To keep a
token out of the container entirely, put a proxy on the host and point the tool
at it.

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
profile/           the coi profile, its provisioning script, and lares-token
```
