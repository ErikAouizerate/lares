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
# Base terminal packages (dots "base_packages", minus what coi-default
# already ships: bat, fd-find, fzf, ripgrep, tree, htop, jq, curl, git)
# ---------------------------------------------------------------------------

apt-get update -qq
apt-get install -y --no-install-recommends \
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
# zsh + oh-my-zsh for the container user (dots base role)
# ---------------------------------------------------------------------------

usermod -s /usr/bin/zsh code

if [ ! -d /home/code/.oh-my-zsh ]; then
  git clone --depth 1 --branch master https://github.com/ohmyzsh/ohmyzsh.git /home/code/.oh-my-zsh
fi
chown -R code:code /home/code/.oh-my-zsh

# ---------------------------------------------------------------------------
# mise runtimes (dots user_tools role + home/.config/mise/config.toml)
#
# mise itself is already in coi-default; only the declared runtimes are
# downloaded here. The config file is symlinked from the dots repository at
# runtime (lares link-dotfiles), and declares the same versions.
# ---------------------------------------------------------------------------

if command -v mise >/dev/null 2>&1; then
  runuser -u code -- env HOME=/home/code \
    mise use --global \
      python@3.14 \
      uv@latest \
      node@24 \
      yarn@latest \
      pnpm@latest \
      rust@stable
fi

echo "lares image provisioning complete"
