#!/bin/sh
# Instala la última versión de bflow en Linux o macOS, sin Go:
#
#   curl -fsSL https://raw.githubusercontent.com/innobytestech/bflow/main/install.sh | sh
#
# BFLOW_INSTALL_DIR cambia la carpeta (por defecto ~/.local/bin).
# BFLOW_VERSION=v0.1.0-rc.9 fija la versión (con o sin la v). Sin ella se instala
# la última; si solo hay prereleases publicadas, la más reciente de ellas.
# Verifica el SHA-256 contra checksums.txt antes de instalar. Después: bflow update.
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

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# get <url>: deja el cuerpo en $tmp/body y devuelve el código HTTP en $code.
get() {
  code="$(curl -sSL -o "$tmp/body" -w '%{http_code}' "$1")" || { echo "bflow: no se pudo consultar $1" >&2; exit 1; }
}
unexpected() { echo "bflow: no se pudo consultar $1 (HTTP $code)" >&2; exit 1; }
tag_of() { tr ',' '\n' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1; }

if [ -n "${BFLOW_VERSION:-}" ]; then
  want_tag="v${BFLOW_VERSION#v}"
  printf '%s' "$want_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || { echo "bflow: BFLOW_VERSION no es una versión válida: $BFLOW_VERSION" >&2; exit 1; }
  get "$api/releases/tags/$want_tag"
  case "$code" in
    200) ;;
    404) echo "bflow: no existe la versión $want_tag" >&2; exit 1 ;;
    *) unexpected "$api/releases/tags/$want_tag" ;;
  esac
  json="$(cat "$tmp/body")"
else
  get "$api/releases/latest"
  case "$code" in
    200) json="$(cat "$tmp/body")" ;;
    404)
      # GitHub no cuenta las prereleases como "latest": con solo prereleases
      # publicadas se toma la más reciente que no sea borrador.
      get "$api/releases?per_page=10"
      [ "$code" = 200 ] || unexpected "$api/releases?per_page=10"
      tags="$(tr ',' '\n' < "$tmp/body" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')"
      json=""
      for t in $tags; do
        get "$api/releases/tags/$t"
        if [ "$code" = 200 ]; then json="$(cat "$tmp/body")"; break; fi
        [ "$code" = 404 ] || unexpected "$api/releases/tags/$t"
      done
      ;;
    *) unexpected "$api/releases/latest" ;;
  esac
fi
tag="$(printf '%s' "$json" | tag_of)"
[ -n "$tag" ] || { echo "bflow: no se encontró una versión publicada" >&2; exit 1; }
asset="bflow_${tag#v}_${os}_${arch}.tar.gz"
url_of() { printf '%s' "$json" | tr ',' '\n' | sed -n "s|.*\"browser_download_url\": *\"\(.*/$1\)\".*|\1|p" | head -n 1; }
asset_url="$(url_of "$asset")"
sums_url="$(url_of checksums.txt)"
[ -n "$asset_url" ] && [ -n "$sums_url" ] || { echo "bflow: $tag no publica $asset" >&2; exit 1; }

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
