#!/usr/bin/env sh
set -eu

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

pm=""
if need_cmd apt-get; then pm="apt"; fi
if need_cmd dnf; then pm="dnf"; fi
if need_cmd pacman; then pm="pacman"; fi
if need_cmd brew; then pm="brew"; fi

if [ -z "$pm" ]; then
  echo "No supported package manager found (apt/dnf/pacman/brew)." >&2
  exit 1
fi

case "$pm" in
  apt)
    sudo apt-get update -y
    # PDF merge tools:
    # - pdftk-java provides "pdftk" on Ubuntu 22.04 (pdftk package is transitional)
    # - qpdf is a reliable fallback
    sudo apt-get install -y --no-install-recommends ca-certificates ffmpeg imagemagick libreoffice calibre poppler-utils pdftk-java qpdf redis-server postgresql
    ;;
  dnf)
    sudo dnf install -y ca-certificates ffmpeg ImageMagick libreoffice calibre poppler-utils qpdf java-17-openjdk-headless redis postgresql-server
    ;;
  pacman)
    sudo pacman -Sy --noconfirm --needed ca-certificates ffmpeg imagemagick libreoffice-fresh calibre poppler qpdf pdftk redis postgresql
    ;;
  brew)
    brew update
    brew install ffmpeg imagemagick libreoffice calibre poppler qpdf pdftk-java redis postgresql@16
    ;;
esac

if need_cmd systemctl; then
  if systemctl list-unit-files 2>/dev/null | grep -q "^redis-server\\.service"; then
    sudo systemctl enable --now redis-server >/dev/null 2>&1 || true
  elif systemctl list-unit-files 2>/dev/null | grep -q "^redis\\.service"; then
    sudo systemctl enable --now redis >/dev/null 2>&1 || true
  fi

  if systemctl list-unit-files 2>/dev/null | grep -q "^postgresql\\.service"; then
    sudo systemctl enable --now postgresql >/dev/null 2>&1 || true
  elif systemctl list-unit-files 2>/dev/null | grep -q "^postgresql-[0-9]+\\.service"; then
    u="$(systemctl list-unit-files 2>/dev/null | awk '/^postgresql-[0-9]+\.service/ {print $1; exit}')"
    if [ -n "$u" ]; then
      sudo systemctl enable --now "$u" >/dev/null 2>&1 || true
    fi
  fi
fi

if ! need_cmd go; then
  echo "Go is not installed. Install Go >= 1.25 manually." >&2
  exit 2
fi

echo "OK"


