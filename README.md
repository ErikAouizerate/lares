# lares

Run coding agents in a [coi](https://github.com/coipond/coi) sandbox, entered
from any repository.

lares is a small wrapper around coi. It owns one sandbox: a persistent coi
container declared by a profile it installs, entered from any repository because
the repositories are mounted at their host path. On top of that it fixes one
thing coi cannot see: copying from inside the container to the clipboard of the
host session.

## Install

From a release, with neither Go nor a repository clone:

```sh
curl -fsSL https://github.com/ErikAouizerate/lares/releases/latest/download/install.sh | sh
```

`install.sh` puts `lares` in `~/.local/bin` (`PREFIX` changes that) and runs
`lares install`, which writes the coi profile. From the repository instead:

```sh
make install          # lares in ~/.local/bin, profile in ~/.coi/profiles/lares
```

Either way it creates `~/.coi/profiles/lares/secrets.env` with empty values.
Fill it with the credentials the sandbox should use before the first
`lares pi`; see [Secrets](#secrets).

Then, once per machine:

```sh
lares build           # build the image the sandbox is made from
lares init            # create the container, once

cd ~/some/repo
lares mount           # let the sandbox see this repository
lares pi              # pi, in this directory, inside the sandbox
```

A container keeps the image it was created from: after `lares build --force`,
recreate it (`coi kill <container>`, then `lares init`) to run the new one. The
repositories and pi's session history are on the host, so nothing of yours goes
with it.

## Commands

| command | what it does |
| --- | --- |
| `lares pi [args…]` | pi in the current directory, which has to be mounted (a shortcut for entering and running pi) |
| `lares bash [args…]` | a login shell in the current directory, prepared like `lares pi` |
| `lares herdr [args…]` | herdr in the current directory |
| `lares run [--] <cmd>` | any command in the current directory, in the same prepared sandbox |
| `lares install [--force]` | install the coi profile, from the files embedded in the binary, and declare the host extras this machine has |
| `lares build [--force]` | build the image the sandbox is made from |
| `lares init` | create the container without entering it |
| `lares start` / `stop` | start the container, stop it keeping its state |
| `lares list` | `coi list` |
| `lares mount [<dir>]` | declare a host directory in the profile and mount it now (the current one by default) |
| `lares unmount [<dir>]` | remove a host directory from the profile and unmount it now (the current one by default) |
| `lares uninstall [--purge]` | remove the sandbox, the profile and the binary |

A repository is mounted with `lares mount`: it is declared in the profile and
mounted on the spot, so the container sees it at its host path and it comes back
with the next container. Nothing is mounted behind your back, so entering a
directory is always a deliberate act. When the current directory is not mounted,
`lares pi` refuses to start — pi works on the repository it is started from —
while `lares bash`, `lares herdr` and `lares run` open in the container home.

## What the profile declares

The profile embedded in the binary carries no host path, so that it installs
itself on any machine. `lares install` adds what this machine actually has:

| host path | mounted as | what it brings |
| --- | --- | --- |
| `~/.local/bin/herdr` | `/usr/local/bin/herdr` | the `herdr` command |
| `~/.agents` | `/home/lares/.agents` | the skills pi loads |
| `~/.pi/agent/extensions` | `/opt/pi/extensions` | the extensions pi loads |

Every one of them is declared only when the host path exists. A machine without
them gets a profile without them, and nothing is created for them — an absent
host path must not keep the container from being created.

Installing again converges rather than duplicating: a mount whose host path is
gone is removed from the profile, and everything else — the repositories you
mounted with `lares mount` in particular — is left alone. `--force` writes the
profile this binary carries, dropping those repositories.

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

### The sandbox user

The image is built from `coi-default`, whose user is `code`; `provision.sh`
renames it and its home to `lares`, and the profile tells coi the new name with
`[incus] code_user`. The uid stays 1000, which is what the image, its sudo rule
and pi's install are built around. Everything lares runs inside the sandbox runs
as that user, and its configuration lives in `/home/lares`.

### The uid Incus has to map

The sandbox runs its commands as the `lares` user of the image, uid 1000. When
the machine's user has another uid, coi maps it into the container, and Incus
only maps a host uid that root has been delegated in `/etc/subuid` and
`/etc/subgid` — as a size-1 range, because Incus refuses a uid inside a wider
block:

```sh
sudo sh -c 'echo "root:$(id -u):1" >> /etc/subuid; echo "root:$(id -u):1" >> /etc/subgid'
sudo systemctl restart incus
```

Without it the container does not start, and Incus answers with a mount error
that names none of this. `lares install` and every command that starts the
container check it and print those two commands instead. A user at uid 1000
needs nothing.

## Releases

`make dist` writes the release into `dist/`: one static binary per platform
(`lares_<version>_linux_{amd64,arm64}.tar.gz`, holding the binary, this README
and `install.sh`), plus `SHA256SUMS` and `install.sh`. `install.sh` reads the
version from the release tag, checks the archive against `SHA256SUMS` and runs
`lares install`.

Publish a release from a clean tree at the tag, so the archives carry the same
version the tag names:

```sh
git tag v0.1.0
make dist
gh release create v0.1.0 dist/* --title v0.1.0
```

## Layout

```
cmd/lares          the command
internal/cli       the commands, the coi profile install, the uid mapping check
internal/coi       calls to coi and incus
internal/ttyproxy  the terminal proxy: PTY, raw mode, resizes
internal/osc52     the OSC 52 filter
internal/clipboard the host clipboard
profile/           the coi profile, its provisioning script, and lares-token
install.sh         the installer a release ships
```
