#!/usr/bin/env bash
set -euo pipefail

ORT_VERSION="${ORT_VERSION:-1.30.0}"
KIND_VERSION="${KIND_VERSION:-v0.24.0}"
TOOLS="${RUNGATED_TOOLS:-$HOME/.local/share/rungated}"
BIN="$HOME/.local/bin"
WHAT="${*:-all}"

mkdir -p "$TOOLS" "$BIN"

have() { command -v "$1" >/dev/null 2>&1; }
say() { printf '\n== %s\n' "$*"; }
want() { case " $WHAT " in *" all "*|*" $1 "*) return 0 ;; *) return 1 ;; esac; }

if ! have dnf; then
  echo "this script is for Fedora (dnf not found)"
  exit 1
fi

if want docker; then
  say "docker"
  if have docker; then
    echo "already installed: $(docker --version)"
  else
    sudo dnf install -y moby-engine docker-compose
  fi
  sudo systemctl enable --now docker
  if ! id -nG "$USER" | grep -qw docker; then
    sudo usermod -aG docker "$USER"
    echo "added $USER to the docker group: log out and back in, or run: newgrp docker"
  fi
fi

if want helm; then
  say "helm"
  if have helm; then echo "already installed: $(helm version --short)"; else sudo dnf install -y helm; fi
fi

if want kubectl; then
  say "kubectl"
  if have kubectl; then echo "already installed"; else sudo dnf install -y kubernetes-client; fi
fi

if want kind; then
  say "kind"
  if have kind; then
    echo "already installed: $(kind version)"
  else
    arch="$(uname -m)"; [ "$arch" = "x86_64" ] && arch=amd64; [ "$arch" = "aarch64" ] && arch=arm64
    curl -fsSL -o "$BIN/kind" "https://kind.sigs.k8s.io/dl/$KIND_VERSION/kind-linux-$arch"
    chmod +x "$BIN/kind"
  fi
fi

if want k6; then
  say "k6"
  if have k6; then
    echo "already installed: $(k6 version)"
  else
    sudo dnf install -y https://dl.k6.io/rpm/repo.rpm
    sudo dnf install -y k6
  fi
fi

if want onnx; then
  say "onnx runtime $ORT_VERSION"
  dir="$TOOLS/onnxruntime"
  if [ -f "$dir/lib/libonnxruntime.so" ]; then
    echo "already installed: $dir"
  else
    tmp="$(mktemp -d)"
    curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v$ORT_VERSION/onnxruntime-linux-x64-$ORT_VERSION.tgz" | tar -xz -C "$tmp"
    rm -rf "$dir"
    mv "$tmp/onnxruntime-linux-x64-$ORT_VERSION" "$dir"
    rm -rf "$tmp"
  fi
  if have gcc; then echo "gcc present"; else sudo dnf install -y gcc; fi
fi

say "path check"
case ":$PATH:" in
  *":$BIN:"*) echo "$BIN is on PATH" ;;
  *) echo "add this to ~/.bashrc then open a new shell:"; echo "export PATH=\"$BIN:\$PATH\"" ;;
esac

say "versions"
for t in docker helm kubectl kind k6; do
  if have "$t"; then printf '%s: ok\n' "$t"; else printf '%s: missing\n' "$t"; fi
done
if [ -f "$TOOLS/onnxruntime/lib/libonnxruntime.so" ]; then echo "onnxruntime: ok ($TOOLS/onnxruntime/lib/libonnxruntime.so)"; else echo "onnxruntime: missing"; fi