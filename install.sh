#!/bin/sh
# Instala la última versión de bflow en Linux o macOS, sin Go:
#
#   curl -fsSL https://raw.githubusercontent.com/innobytestech/bflow/main/install.sh | sh
#
# BFLOW_INSTALL_DIR cambia la carpeta (por defecto ~/.local/bin). Verifica el
# SHA-256 contra checksums.txt antes de instalar. Después: bflow update.
set -eu

api="${BFLOW_RELEASES_URL:-https://api.github.com/repos/innobytestech/bflow}"
dir="${BFLOW_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "bflow: sistema no soportado: $(uname -s); en Windows usa install.ps1" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "bflow: arquitectura no soportada: $(uname -m)" >&2; exit 1 ;;
esac

json="$(curl -fsSL "$api/releases/latest")"
tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
[ -n "$tag" ] || { echo "bflow: no se encontró una versión publicada" >&2; exit 1; }
asset="bflow_${tag#v}_${os}_${arch}.tar.gz"
url_of() { printf '%s' "$json" | tr ',' '\n' | sed -n "s|.*\"browser_download_url\": *\"\(.*/$1\)\".*|\1|p" | head -n 1; }
asset_url="$(url_of "$asset")"
sums_url="$(url_of checksums.txt)"
[ -n "$asset_url" ] && [ -n "$sums_url" ] || { echo "bflow: $tag no publica $asset" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset" "$asset_url"
curl -fsSL -o "$tmp/checksums.txt" "$sums_url"
want="$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else
  got="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
fi
[ -n "$want" ] && [ "$got" = "$want" ] || { echo "bflow: $asset no coincide con checksums.txt; no se instala" >&2; exit 1; }

tar -xzf "$tmp/$asset" -C "$tmp" bflow
mkdir -p "$dir"
mv "$tmp/bflow" "$dir/bflow"
chmod 755 "$dir/bflow"
echo "bflow $tag instalado en $dir/bflow"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Agrega $dir a tu PATH (por ejemplo en ~/.profile): export PATH=\"$dir:\$PATH\"" ;;
esac
