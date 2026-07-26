package coord

import (
	"net/http"
	"os"
	"text/template"
)

// installScriptTemplate is the universal provider install script served at /install-provider.sh.
// {{.DownloadBase}} is substituted at serve time from the MESH_INSTALL_SCRIPT_DOWNLOAD_BASE
// env var, defaulting to the GitHub releases download URL.
const installScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

# Detect OS and arch, map to goreleaser naming.
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "Unsupported arch: $ARCH"; exit 1 ;;
esac
case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

DOWNLOAD_BASE="{{.DownloadBase}}"
ARCHIVE="gpumesh-provider_${OS}_${ARCH}.tar.gz"
URL="${DOWNLOAD_BASE}/${ARCHIVE}"

echo "→ Downloading gpumesh-provider for $OS/$ARCH..."
echo "→ Fetching $URL"
if command -v curl >/dev/null 2>&1; then
  curl -sSfL "$URL" -o "/tmp/${ARCHIVE}"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$URL" -O "/tmp/${ARCHIVE}"
else
  echo "Error: curl or wget required."
  exit 1
fi

# Extract and install.
cd /tmp
tar xzf "$ARCHIVE"
INSTALL_DIR="${PREFIX:-/usr/local/bin}"
sudo mv gpumesh-provider "$INSTALL_DIR/" 2>/dev/null || {
  echo "→ Installing to ~/.local/bin (no sudo)"
  mkdir -p ~/.local/bin
  mv gpumesh-provider ~/.local/bin/
  echo "→ Add ~/.local/bin to your PATH if not already there."
}

INSTALLED=$(which gpumesh-provider 2>/dev/null || echo "$INSTALL_DIR/gpumesh-provider")
echo "✓ gpumesh-provider installed to $INSTALLED"
echo ""
echo "Next steps:"
echo "  Run: gpumesh-provider"
echo "  The setup wizard will guide you through configuration."
`

var installScriptTmpl = template.Must(template.New("install").Parse(installScriptTemplate))

// handleInstallScript serves the universal provider install script.
// The download base URL is configured via MESH_INSTALL_SCRIPT_DOWNLOAD_BASE env var,
// defaulting to the GitHub releases download URL.
func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	downloadBase := os.Getenv("MESH_INSTALL_SCRIPT_DOWNLOAD_BASE")
	if downloadBase == "" {
		downloadBase = "https://github.com/r00takaspin/gpumesh/releases/latest/download"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := installScriptTmpl.Execute(w, map[string]string{
		"DownloadBase": downloadBase,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
