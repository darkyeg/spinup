#!/bin/sh
# Installs the latest spinup release into ~/.local/bin, checksum-verified.
#   curl -fsSL https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.sh | sh
set -eu

repo="${SPINUP_RELEASES:-darkyeg/spinup}"
case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "spinup: unsupported OS $(uname -s); on Windows use install.ps1" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "spinup: unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac

asset="spinup_${os}_${arch}"
base="https://github.com/${repo}/releases/latest/download"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A progress bar needs a terminal; piped into a log, -# would write thousands of lines.
if [ -t 2 ]; then progress=-#; else progress=-s; fi

echo "Downloading $asset from $repo..."
if ! curl -fL $progress -o "$tmp/$asset" "$base/$asset"; then
  echo "spinup: could not download $asset." >&2
  echo "  Check your connection, and that $repo has a release with that file:" >&2
  echo "  https://github.com/${repo}/releases/latest" >&2
  exit 1
fi
if ! curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"; then
  echo "spinup: $repo's latest release has no checksums.txt, so the download cannot be verified." >&2
  exit 1
fi

echo "Checking the download..."
want="$(awk -v a="$asset" '$2 == a {print $1}' "$tmp/checksums.txt")"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi
if [ -z "$want" ]; then
  echo "spinup: checksums.txt does not list $asset; nothing installed" >&2
  exit 1
fi
if [ "$want" != "$got" ]; then
  echo "spinup: checksum mismatch for $asset; nothing installed" >&2
  echo "  expected $want" >&2
  echo "  got      $got" >&2
  exit 1
fi

dest="$HOME/.local/bin"
mkdir -p "$dest"
install -m 0755 "$tmp/$asset" "$dest/spinup"
echo "Installed $("$dest/spinup" --version) to $dest/spinup"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "Add $dest to your PATH, then open a new terminal." ;;
esac
echo "Next: spinup setup <name-for-this-machine>"
