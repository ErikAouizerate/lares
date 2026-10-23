#!/bin/bash
# Provisioning script for the "lares" Coi image (coi-lares).
#
# Runs as root inside a build container launched from coi-default (Ubuntu
# noble). Idempotent: re-running it converges instead of duplicating.
#
# Terminal-only tooling, adapted from the "vps" profile of the dots
# repository (which targets Fedora/dnf; this is the apt equivalent).
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# ---------------------------------------------------------------------------
# The sandbox user
#
# coi's base image creates "code"; the sandbox calls its user "lares", so the
# name inside the container says what the sandbox is. The uid stays 1000:
# coi-default, its sudo rule and everything installed for the user are built
# around it, and only the name and the home change.
# ---------------------------------------------------------------------------

if id code >/dev/null 2>&1; then
  usermod -l lares -d /home/lares -m code
  groupmod -n lares code

  # coi's passwordless sudo rule names the user.
  echo "lares ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/lares
  chown root:root /etc/sudoers.d/lares
  chmod 0440 /etc/sudoers.d/lares
  rm -f /etc/sudoers.d/code

  # pi records where it installed itself before the rename.
  manifest=/home/lares/.pi/agent/install/managed-install.json
  [ -f "$manifest" ] && sed -i 's|/home/code|/home/lares|g' "$manifest"

  # Absolute symlinks point at the old home: /usr/local/bin is where coi puts the
  # agent launchers that have to stay on PATH, and the user's bin is where the
  # installers left theirs.
  for link in /usr/local/bin/* /home/lares/.local/bin/*; do
    [ -L "$link" ] || continue

    target=$(readlink "$link")
    case "$target" in
      /home/code/*) ln -sfn "/home/lares/${target#/home/code/}" "$link" ;;
    esac
  done
fi

# ---------------------------------------------------------------------------
# Base terminal packages (dots "base_packages", minus what coi-default
# already ships: bat, fd-find, fzf, ripgrep, tree, htop, jq, curl, git)
# ---------------------------------------------------------------------------

# The base image can still have a boot-time upgrade holding the dpkg lock when
# this script starts; DPkg::Lock::Timeout makes apt-get wait for it instead of
# failing the build outright.
APT_GET="apt-get -o DPkg::Lock::Timeout=180"

$APT_GET update -qq
$APT_GET install -y --no-install-recommends \
  direnv \
  zsh

# Ubuntu ships `bat`/`fd` under the names batcat/fdfind. coi renames them in
# its base image; make sure the plain names exist anyway.
[ -x /usr/bin/batcat ] && ln -sf /usr/bin/batcat /usr/local/bin/bat
[ -x /usr/bin/fdfind ] && ln -sf /usr/bin/fdfind /usr/local/bin/fd

# gitleaks has no Ubuntu package: pinned release binary from upstream.
GITLEAKS_VERSION="8.30.1"
case "$(dpkg --print-architecture)" in
  amd64) GITLEAKS_ARCH="x64" ;;
  arm64) GITLEAKS_ARCH="arm64" ;;
  *)     GITLEAKS_ARCH="x64" ;;
esac
if ! command -v gitleaks >/dev/null 2>&1; then
  curl -fsSL "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_${GITLEAKS_ARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin gitleaks
  chmod 0755 /usr/local/bin/gitleaks
fi

# ---------------------------------------------------------------------------
# zsh + oh-my-zsh for the sandbox user (dots base role)
# ---------------------------------------------------------------------------

usermod -s /usr/bin/zsh lares

if [ ! -d /home/lares/.oh-my-zsh ]; then
  git clone --depth 1 --branch master https://github.com/ohmyzsh/ohmyzsh.git /home/lares/.oh-my-zsh
fi
chown -R lares:lares /home/lares/.oh-my-zsh

# ---------------------------------------------------------------------------
# mise runtimes (dots user_tools role + home/.config/mise/config.toml)
#
# mise itself is already in coi-default; only the declared runtimes are
# downloaded here. The config file is the sandbox user's, and declares the
# same versions.
# ---------------------------------------------------------------------------

if command -v mise >/dev/null 2>&1; then
  runuser -u lares -- env HOME=/home/lares \
    mise use --global \
      python@3.14 \
      uv@latest \
      node@24 \
      yarn@latest \
      pnpm@latest \
      rust@stable
fi

echo "lares image provisioning complete"
