#!/usr/bin/env sh

set -eu

repo="${TEACRUSH_REPO:-zeozeozeo/teacrush}"
tag="${TEACRUSH_TAG:-nightly}"
install_dir="${TEACRUSH_INSTALL_DIR:-$HOME/.local/bin}"
action="${1:-install}"

fail() {
    echo "teacrush: $*" >&2
    exit 1
}

case "$action" in
    install)
        ;;
    uninstall|--uninstall)
        binary="$install_dir/teacrush"
        if [ -e "$binary" ]; then
            rm -f "$binary"
            echo "Removed $binary"
        else
            echo "teacrush is not installed at $binary"
        fi
        # rmdir only succeeds when the directory is empty, so other files are safe.
        if [ -d "$install_dir" ]; then
            rmdir "$install_dir" 2>/dev/null || true
        fi
        echo "No shell profiles were changed by this installer. Remove $install_dir from PATH manually if you added it."
        exit 0
        ;;
    *)
        fail "usage: $0 [uninstall|--uninstall]"
        ;;
esac

if command -v curl >/dev/null 2>&1; then
    download() {
        curl -fsSL "$1" -o "$2"
    }
elif command -v wget >/dev/null 2>&1; then
    download() {
        wget -qO "$2" "$1"
    }
else
    fail "curl or wget is required"
fi

os="$(uname -s)"
case "$os" in
    Linux) platform=linux ;;
    Darwin) platform=darwin ;;
    *) fail "unsupported operating system: $os" ;;
esac

arch="$(uname -m)"
case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) fail "unsupported architecture: $arch" ;;
esac

asset="teacrush-$tag-$platform-$arch.tar.gz"
base_url="https://github.com/$repo/releases/download/$tag"
temporary_dir="$(mktemp -d 2>/dev/null || mktemp -d -t teacrush)"
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

echo "Downloading $asset..."
download "$base_url/$asset" "$temporary_dir/$asset" || fail "could not download $asset from $base_url"
download "$base_url/SHA256SUMS" "$temporary_dir/SHA256SUMS" || fail "could not download checksum file"

expected="$(awk -v file="$asset" '$2 == file { print $1 }' "$temporary_dir/SHA256SUMS")"
[ -n "$expected" ] || fail "checksum for $asset was not found"

if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$temporary_dir/$asset" | awk '{ print $1}')"
elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$temporary_dir/$asset" | awk '{ print $1}')"
else
    fail "sha256sum or shasum is required to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum verification failed"

mkdir -p "$install_dir"
tar -xzf "$temporary_dir/$asset" -C "$temporary_dir"
install -m 0755 "$temporary_dir/teacrush" "$install_dir/teacrush"

echo "Installed teacrush to $install_dir/teacrush"
case ":${PATH:-}:" in
    *:"$install_dir":*) ;;
    *) echo "Add $install_dir to PATH to run teacrush from any shell." ;;
esac
