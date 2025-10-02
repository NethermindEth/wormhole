#!/usr/bin/env bash
set -euo pipefail

# Wormhole devnet setup for macOS
# This replaces the Linux-only scripts/dev-setup.sh

# Refuse to run as root
if [[ $EUID -eq 0 ]]; then
    echo "This script must not be run as root" 1>&2
    exit 1
fi

echo "👉 Installing core dependencies with Homebrew..."

brew install go@1.23 || true
brew install kubernetes-cli || true
brew install minikube || true
brew install tilt-dev/tap/tilt || true
brew install docker || true
brew install grpcurl || true

# Ensure Go 1.23 is in PATH
if ! grep -q "/opt/homebrew/opt/go@1.23/bin" "$HOME/.zshrc"; then
  echo 'export PATH="/opt/homebrew/opt/go@1.23/bin:$PATH"' >> "$HOME/.zshrc"
  echo "⚠️ Added Go 1.23 to PATH in ~/.zshrc. Run 'source ~/.zshrc' or open a new shell."
fi

echo "👉 Starting Docker Desktop (make sure it's installed and running)..."
open -ga Docker

echo "👉 Starting Minikube with Docker driver..."
minikube start --cpus=6 --memory=7000m --disk-size=50g --driver=docker

echo "👉 Creating wormhole namespace..."
kubectl create namespace wormhole || true
kubectl config set-context --current --namespace=wormhole

echo "👉 All prerequisites installed. To start the Wormhole devnet, run:"
echo "   tilt up"
echo
echo "Tilt UI will open at http://localhost:10350"
echo "Check pods with: kubectl get pods -n wormhole"
echo
echo "👉 To watch VAAs via Spy:"
echo "   kubectl port-forward svc/spy 7073:7073"
echo "   grpcurl -plaintext localhost:7073 spy.v1.SpyService.SubscribeSignedVAA"