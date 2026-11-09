#!/usr/bin/env bash
set -euo pipefail

# Unified build script for shareme -- builds desktop + agent and packages for release.
#
# Two modes:
#   - All-targets mode (default): builds desktop for --platforms, agent for all 6 targets,
#     packages for --platforms. Use for local release prep.
#   - Single-platform CI mode (--platform): builds desktop + agent + package for one platform only.
#     Use in GitHub Actions matrix jobs.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

DESKTOP_ONLY=false
AGENT_ONLY=false
SKIP_PACKAGE=false
DESKTOP_PLATFORMS=""
CI_PLATFORM=""

case "$(uname -s)" in
    Darwin) DEFAULT_PLATFORM="darwin/universal" ;;
    Linux)  DEFAULT_PLATFORM="linux/amd64" ;;
    *)      DEFAULT_PLATFORM="linux/amd64" ;;
esac

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "CI mode (single platform):"
    echo "  --platform <p>     Build desktop + agent + package for one platform"
    echo "                     Values: windows/amd64, linux/amd64, linux/arm64,"
    echo "                             darwin/amd64, darwin/arm64, darwin/universal"
    echo ""
    echo "All-targets mode (default):"
    echo "  --desktop          Build desktop only"
    echo "  --agent            Build agent only"
    echo "  --no-package       Skip packaging"
    echo "  --platforms <list> Comma-separated platforms (default: $DEFAULT_PLATFORM)"
    echo "  --help             Show this help"
    echo ""
    echo "Examples:"
    echo "  $0                                    # Local: build all and package"
    echo "  $0 --platform linux/amd64             # CI: single platform"
    echo "  $0 --platform darwin/universal --no-package"
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --platform)    CI_PLATFORM="$2"; shift ;;
        --desktop)     DESKTOP_ONLY=true ;;
        --agent)       AGENT_ONLY=true ;;
        --no-package)  SKIP_PACKAGE=true ;;
        --platforms)   DESKTOP_PLATFORMS="$2"; shift ;;
        --help)        usage ;;
        *)             echo "Unknown option: $1"; usage ;;
    esac
    shift
done

# --- CI mode ---
if [ -n "$CI_PLATFORM" ]; then
    echo ""
    echo "========== CI Build: $CI_PLATFORM =========="

    # Desktop
    echo ""
    echo "--- Desktop ---"
    "$SCRIPT_DIR/build-desktop.sh" "$CI_PLATFORM"

    # Agent
    echo ""
    echo "--- Agent ---"
    os="${CI_PLATFORM%%/*}"
    arch="${CI_PLATFORM##*/}"

    if [ "$CI_PLATFORM" = "darwin/universal" ]; then
        GOOS=darwin GOARCH=amd64 GOTELEMETRY=off CGO_ENABLED=0 \
            "$SCRIPT_DIR/build-agent.sh" "$PROJECT_DIR/dist/shareme-agent-darwin-amd64"
        GOOS=darwin GOARCH=arm64 GOTELEMETRY=off CGO_ENABLED=0 \
            "$SCRIPT_DIR/build-agent.sh" "$PROJECT_DIR/dist/shareme-agent-darwin-arm64"
    else
        ext=""
        [ "$os" = "windows" ] && ext=".exe"
        GOOS="$os" GOARCH="$arch" GOTELEMETRY=off CGO_ENABLED=0 \
            "$SCRIPT_DIR/build-agent.sh" "$PROJECT_DIR/dist/shareme-agent-${os}-${arch}${ext}"
    fi

    # Package
    if ! $SKIP_PACKAGE; then
        echo ""
        echo "--- Package ---"
        package_platform "$CI_PLATFORM"
    fi

    echo ""
    echo "========== Done: dist/release/ =========="
    exit 0
fi

# --- All-targets mode ---
DESKTOP_PLATFORMS="${DESKTOP_PLATFORMS:-$DEFAULT_PLATFORM}"

BUILD_ALL=false
$DESKTOP_ONLY || $AGENT_ONLY || BUILD_ALL=true

AGENT_TARGETS=(
    "windows/amd64"
    "windows/arm64"
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

build_desktop() {
    local platform="$1"
    echo ""
    echo "=== Building desktop for $platform ==="
    "$SCRIPT_DIR/build-desktop.sh" "$platform"
}

build_agent() {
    local goos="$1"
    local goarch="$2"
    local ext=""
    [ "$goos" = "windows" ] && ext=".exe"
    local out="$PROJECT_DIR/dist/shareme-agent-${goos}-${goarch}${ext}"
    echo ""
    echo "=== Building agent for $goos/$goarch -> $out ==="
    GOOS="$goos" GOARCH="$goarch" GOTELEMETRY=off CGO_ENABLED=0 \
        "$SCRIPT_DIR/build-agent.sh" "$out"
}

package_platform() {
    local platform="$1"

    local name ext desktop agent
    case "$platform" in
        windows/amd64) name="shareme-windows-amd64"; ext=".zip";    desktop="backend/build/bin/shareme.exe"; agent="dist/shareme-agent-windows-amd64.exe" ;;
        windows/arm64) name="shareme-windows-arm64"; ext=".zip";    desktop="backend/build/bin/shareme.exe"; agent="dist/shareme-agent-windows-arm64.exe" ;;
        linux/amd64)   name="shareme-linux-amd64";   ext=".tar.gz"; desktop="backend/build/bin/shareme";    agent="dist/shareme-agent-linux-amd64" ;;
        linux/arm64)   name="shareme-linux-arm64";   ext=".tar.gz"; desktop="backend/build/bin/shareme";    agent="dist/shareme-agent-linux-arm64" ;;
        darwin/amd64)  name="shareme-darwin-amd64";  ext=".zip";    desktop="backend/build/bin/shareme.app"; agent="" ;;
        darwin/arm64)  name="shareme-darwin-arm64";  ext=".zip";    desktop="backend/build/bin/shareme.app"; agent="" ;;
        darwin/universal) name="shareme-darwin-universal"; ext=".zip"; desktop="backend/build/bin/shareme.app"; agent="" ;;
        *)
            echo "Warning: unknown platform for packaging: $platform"
            return
            ;;
    esac

    local release_dir="$PROJECT_DIR/dist/release"
    local pkg_dir="$PROJECT_DIR/dist/package/$name"

    mkdir -p "$release_dir" "$pkg_dir"

    if [ -e "$PROJECT_DIR/$desktop" ]; then
        cp -R "$PROJECT_DIR/$desktop" "$pkg_dir/"
    else
        echo "Warning: desktop artifact not found: $desktop"
    fi

    if [ "$platform" = "darwin/universal" ]; then
        local amd64_bin="$PROJECT_DIR/dist/shareme-agent-darwin-amd64"
        local arm64_bin="$PROJECT_DIR/dist/shareme-agent-darwin-arm64"
        if [ -f "$amd64_bin" ] && [ -f "$arm64_bin" ]; then
            echo "Creating universal agent via lipo ..."
            lipo -create -output "$pkg_dir/shareme-agent" "$amd64_bin" "$arm64_bin"
        else
            echo "Warning: universal agent skipped: missing darwin/amd64 or darwin/arm64 agent"
        fi
    elif [ "$platform" = "darwin/amd64" ]; then
        cp "$PROJECT_DIR/dist/shareme-agent-darwin-amd64" "$pkg_dir/shareme-agent"
    elif [ "$platform" = "darwin/arm64" ]; then
        cp "$PROJECT_DIR/dist/shareme-agent-darwin-arm64" "$pkg_dir/shareme-agent"
    else
        if [ -e "$PROJECT_DIR/$agent" ]; then
            cp "$PROJECT_DIR/$agent" "$pkg_dir/"
        else
            echo "Warning: agent artifact not found: $agent"
        fi
    fi

    local archive="$release_dir/$name$ext"
    echo "Packaging $archive ..."

    case "$platform" in
        linux/*) tar -czf "$archive" -C "$pkg_dir" . ;;
        *)       (cd "$pkg_dir" && zip -qr "$archive" .) ;;
    esac

    echo "Package created: $archive"
}

# --- Main (all-targets) ---

IFS=',' read -ra platforms <<< "$DESKTOP_PLATFORMS"

if $BUILD_ALL || $DESKTOP_ONLY; then
    echo ""
    echo "========== Building Desktop =========="
    for p in "${platforms[@]}"; do
        p="${p#"${p%%[![:space:]]*}"}"
        p="${p%"${p##*[![:space:]]}"}"
        [ -n "$p" ] && build_desktop "$p"
    done
fi

if $BUILD_ALL || $AGENT_ONLY; then
    echo ""
    echo "========== Building Agent =========="
    for t in "${AGENT_TARGETS[@]}"; do
        goos="${t%/*}"
        goarch="${t#*/}"
        build_agent "$goos" "$goarch"
    done
fi

if ! $SKIP_PACKAGE; then
    echo ""
    echo "========== Packaging =========="
    for p in "${platforms[@]}"; do
        p="${p#"${p%%[![:space:]]*}"}"
        p="${p%"${p##*[![:space:]]}"}"
        [ -n "$p" ] && package_platform "$p"
    done
    echo ""
    echo "========== Release assets in dist/release/ =========="
fi

