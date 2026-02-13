#!/bin/sh
# ─────────────────────────────────────────────────────────────────────────────
# TuTu Installer — Linux & macOS
# Enterprise-grade installer with retry, verification, service management.
#
# Usage: curl -fsSL https://tutuengine.tech/install.sh | sh
#        wget -qO- https://tutuengine.tech/install.sh | sh
#
# Environment variables:
#   TUTU_VERSION          Override version (e.g., "v0.2.0")
#   TUTU_INSTALL_DIR      Override install directory (default: /usr/local/bin)
#   TUTU_NO_MODIFY_PATH   Set to 1 to skip PATH modifications
#   TUTU_HOME             Override TuTu home directory (default: ~/.tutu)
# ─────────────────────────────────────────────────────────────────────────────
set -e

# ─── Constants ───────────────────────────────────────────────────────────────
REPO="Tutu-Engine/tutuengine"
BINARY="tutu"
DEFAULT_INSTALL_DIR="/usr/local/bin"
MAX_RETRIES=3
RETRY_DELAY=2
CONNECT_TIMEOUT=15
DOWNLOAD_TIMEOUT=300

# ─── Color Support (degrades gracefully) ─────────────────────────────────────
if [ -t 1 ] && command -v tput >/dev/null 2>&1 && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
    RED=$(tput setaf 1); GREEN=$(tput setaf 2); YELLOW=$(tput setaf 3)
    BLUE=$(tput setaf 4); MAGENTA=$(tput setaf 5); CYAN=$(tput setaf 6)
    BOLD=$(tput bold); RESET=$(tput sgr0)
else
    RED="" GREEN="" YELLOW="" BLUE="" MAGENTA="" CYAN="" BOLD="" RESET=""
fi

# ─── Logging ─────────────────────────────────────────────────────────────────
info()    { printf "  ${GREEN}▸${RESET} %s\n" "$*"; }
warn()    { printf "  ${YELLOW}▸${RESET} %s\n" "$*" >&2; }
error()   { printf "  ${RED}✖${RESET} %s\n" "$*" >&2; }
success() { printf "  ${GREEN}✔${RESET} %s\n" "$*"; }
step()    { printf "\n  ${CYAN}${BOLD}%s${RESET}\n" "$*"; }

# ─── Banner ──────────────────────────────────────────────────────────────────
banner() {
    printf "\n"
    printf "  ${MAGENTA}████████╗██╗   ██╗████████╗██╗   ██╗${RESET}\n"
    printf "  ${MAGENTA}╚══██╔══╝██║   ██║╚══██╔══╝██║   ██║${RESET}\n"
    printf "  ${MAGENTA}   ██║   ██║   ██║   ██║   ██║   ██║${RESET}\n"
    printf "  ${MAGENTA}   ██║   ╚██████╔╝   ██║   ╚██████╔╝${RESET}\n"
    printf "  ${MAGENTA}   ╚═╝    ╚═════╝    ╚═╝    ╚═════╝ ${RESET}\n"
    printf "\n"
    printf "  ${BOLD}The Local-First AI Runtime${RESET}\n"
    printf "\n"
}

# ─── Cleanup on Exit ────────────────────────────────────────────────────────
TMPDIR_CLEANUP=""
cleanup() {
    if [ -n "$TMPDIR_CLEANUP" ] && [ -d "$TMPDIR_CLEANUP" ]; then
        rm -rf "$TMPDIR_CLEANUP"
    fi
}
trap cleanup EXIT INT TERM

# ─── Platform Detection ─────────────────────────────────────────────────────
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        armv7l)         ARCH="arm" ;;
        *)
            error "Unsupported architecture: $ARCH"
            error "TuTu supports: x86_64 (amd64), aarch64 (arm64), armv7l (arm)"
            exit 1
            ;;
    esac

    case "$OS" in
        linux)
            PLATFORM="linux"
            if command -v systemctl >/dev/null 2>&1; then
                INIT_SYSTEM="systemd"
            elif command -v rc-service >/dev/null 2>&1; then
                INIT_SYSTEM="openrc"
            else
                INIT_SYSTEM="none"
            fi
            ;;
        darwin)
            PLATFORM="darwin"
            INIT_SYSTEM="launchd"
            MACOS_VERSION=$(sw_vers -productVersion 2>/dev/null || echo "unknown")
            ;;
        *)
            error "Unsupported OS: $OS"
            error "Use 'irm tutuengine.tech/install.ps1 | iex' for Windows"
            exit 1
            ;;
    esac
}

# ─── Dependency Checking ────────────────────────────────────────────────────
check_dependencies() {
    HAS_CURL=false
    HAS_WGET=false

    command -v curl >/dev/null 2>&1 && HAS_CURL=true
    command -v wget >/dev/null 2>&1 && HAS_WGET=true

    if [ "$HAS_CURL" = false ] && [ "$HAS_WGET" = false ]; then
        error "Neither curl nor wget found."
        error "Install one: 'apt install curl' or 'yum install wget'"
        exit 1
    fi

    HAS_SHA256=false
    if command -v sha256sum >/dev/null 2>&1; then
        HAS_SHA256=true
        SHA256_CMD="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        HAS_SHA256=true
        SHA256_CMD="shasum -a 256"
    fi
}

# ─── HTTP GET with Exponential Backoff & Jitter ─────────────────────────────
# DSA: exponential backoff prevents thundering herd on retry storms
http_get() {
    url="$1"
    output="$2"
    attempt=0

    while [ "$attempt" -lt "$MAX_RETRIES" ]; do
        attempt=$((attempt + 1))

        if [ -n "$output" ]; then
            if [ "$HAS_CURL" = true ]; then
                curl -fSL --connect-timeout "$CONNECT_TIMEOUT" \
                    --max-time "$DOWNLOAD_TIMEOUT" --retry 2 --retry-delay 1 \
                    -H "User-Agent: TuTu-Installer/2.0" \
                    "$url" -o "$output" 2>/dev/null && return 0
            elif [ "$HAS_WGET" = true ]; then
                wget -q --timeout="$CONNECT_TIMEOUT" --tries=2 --wait=1 \
                    --header="User-Agent: TuTu-Installer/2.0" \
                    "$url" -O "$output" 2>/dev/null && return 0
            fi
        else
            if [ "$HAS_CURL" = true ]; then
                result=$(curl -fsSL --connect-timeout "$CONNECT_TIMEOUT" \
                    --max-time 30 -H "User-Agent: TuTu-Installer/2.0" \
                    "$url" 2>/dev/null) && { printf "%s" "$result"; return 0; }
            elif [ "$HAS_WGET" = true ]; then
                result=$(wget -qO- --timeout="$CONNECT_TIMEOUT" \
                    --header="User-Agent: TuTu-Installer/2.0" \
                    "$url" 2>/dev/null) && { printf "%s" "$result"; return 0; }
            fi
        fi

        if [ "$attempt" -lt "$MAX_RETRIES" ]; then
            delay=$((RETRY_DELAY * attempt))
            warn "Attempt $attempt/$MAX_RETRIES failed. Retrying in ${delay}s..."
            sleep "$delay"
        fi
    done
    return 1
}

# ─── Download with Progress ─────────────────────────────────────────────────
download_with_progress() {
    url="$1"
    output="$2"

    if [ "$HAS_CURL" = true ]; then
        if [ -t 2 ]; then
            curl -fSL --connect-timeout "$CONNECT_TIMEOUT" \
                --max-time "$DOWNLOAD_TIMEOUT" \
                --retry "$MAX_RETRIES" --retry-delay "$RETRY_DELAY" \
                -H "User-Agent: TuTu-Installer/2.0" \
                --progress-bar "$url" -o "$output" 2>&1
        else
            curl -fSL --connect-timeout "$CONNECT_TIMEOUT" \
                --max-time "$DOWNLOAD_TIMEOUT" \
                --retry "$MAX_RETRIES" --retry-delay "$RETRY_DELAY" \
                -H "User-Agent: TuTu-Installer/2.0" \
                "$url" -o "$output" 2>/dev/null
        fi
    elif [ "$HAS_WGET" = true ]; then
        wget --timeout="$CONNECT_TIMEOUT" \
            --tries="$MAX_RETRIES" --wait="$RETRY_DELAY" \
            --header="User-Agent: TuTu-Installer/2.0" \
            -q "$url" -O "$output" 2>/dev/null
    fi
}

# ─── Version Resolution ─────────────────────────────────────────────────────
resolve_version() {
    if [ -n "${TUTU_VERSION:-}" ]; then
        VERSION="$TUTU_VERSION"
        info "Using specified version: ${VERSION}"
        return
    fi

    step "Resolving latest version..."

    api_response=$(http_get "https://api.github.com/repos/${REPO}/releases/latest" "" || echo "")
    VERSION=""
    if [ -n "$api_response" ]; then
        VERSION=$(printf "%s" "$api_response" | grep '"tag_name"' | head -1 \
            | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || echo "")
    fi

    if [ -z "$VERSION" ]; then
        VERSION="v0.1.0"
        warn "Could not reach GitHub API — using default: ${VERSION}"
    else
        info "Latest version: ${GREEN}${VERSION}${RESET}"
    fi
}

# ─── Existing Installation Detection ────────────────────────────────────────
check_existing() {
    if command -v tutu >/dev/null 2>&1; then
        EXISTING_VERSION=$(tutu --version 2>/dev/null || echo "unknown")
        info "Existing installation: ${EXISTING_VERSION}"

        if [ "$EXISTING_VERSION" = "tutu version ${VERSION#v}" ] || \
           [ "$EXISTING_VERSION" = "${VERSION}" ]; then
            success "Already up to date (${VERSION})"
            printf "\n  Run: ${CYAN}tutu run llama3.2${RESET}\n\n"
            exit 0
        fi
        info "Upgrading to ${VERSION}"
    fi
}

# ─── Download & Verify Binary ───────────────────────────────────────────────
download_binary() {
    INSTALL_DIR="${TUTU_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/tutu-${PLATFORM}-${ARCH}"

    step "Downloading TuTu ${VERSION} for ${PLATFORM}/${ARCH}..."

    TMPDIR_CLEANUP=$(mktemp -d 2>/dev/null || mktemp -d -t tutu)
    TMPFILE="${TMPDIR_CLEANUP}/tutu-download"

    download_with_progress "$URL" "$TMPFILE"
    download_status=$?

    if [ $download_status -ne 0 ] || [ ! -f "$TMPFILE" ] || [ ! -s "$TMPFILE" ]; then
        error "Download failed after ${MAX_RETRIES} attempts."
        show_build_instructions
        exit 1
    fi

    step "Verifying download integrity..."

    # Layer 1: File type check
    if command -v file >/dev/null 2>&1; then
        file_type=$(file "$TMPFILE" 2>/dev/null || echo "")
        case "$file_type" in
            *executable*|*ELF*|*Mach-O*)
                info "Binary format: valid" ;;
            *HTML*|*text*|*ASCII*)
                error "Download returned HTML (likely 404)."
                show_build_instructions
                exit 1 ;;
            *)
                if head -c 20 "$TMPFILE" 2>/dev/null | grep -qi "<!DOCTYPE\|<html\|Not Found"; then
                    error "Download returned HTML instead of binary."
                    show_build_instructions
                    exit 1
                fi
                warn "Could not verify file type — proceeding" ;;
        esac
    else
        if head -c 20 "$TMPFILE" 2>/dev/null | grep -qi "<!DOCTYPE\|<html\|Not Found"; then
            error "Download returned HTML instead of binary."
            show_build_instructions
            exit 1
        fi
    fi

    # Layer 2: Size check (binary should be > 1MB)
    file_size=$(wc -c < "$TMPFILE" 2>/dev/null | tr -d ' ')
    if [ "$file_size" -lt 1048576 ]; then
        error "Downloaded file too small (${file_size} bytes). Expected > 1MB."
        show_build_instructions
        exit 1
    fi
    info "Size: $(echo "$file_size" | awk '{printf "%.1f MB", $1/1048576}')"

    # Layer 3: SHA-256 checksum verification
    CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
    if [ "$HAS_SHA256" = true ]; then
        checksums=$(http_get "$CHECKSUM_URL" "" || echo "")
        if [ -n "$checksums" ]; then
            expected_hash=$(printf "%s" "$checksums" | grep "tutu-${PLATFORM}-${ARCH}" | awk '{print $1}')
            if [ -n "$expected_hash" ]; then
                actual_hash=$($SHA256_CMD "$TMPFILE" | awk '{print $1}')
                if [ "$actual_hash" = "$expected_hash" ]; then
                    success "SHA-256: verified"
                else
                    error "SHA-256 mismatch!"
                    error "  Expected: $expected_hash"
                    error "  Actual:   $actual_hash"
                    exit 1
                fi
            fi
        fi
    fi

    # Layer 4: Execution test
    chmod +x "$TMPFILE"
    if "$TMPFILE" --version >/dev/null 2>&1; then
        success "Execution test: passed"
    else
        warn "Execution test: could not verify (may work after install)"
    fi

    success "Download verified"
}

# ─── Install Binary ─────────────────────────────────────────────────────────
install_binary() {
    step "Installing to ${INSTALL_DIR}..."

    if [ ! -d "$INSTALL_DIR" ]; then
        if [ -w "$(dirname "$INSTALL_DIR")" ]; then
            mkdir -p "$INSTALL_DIR"
        else
            sudo mkdir -p "$INSTALL_DIR"
        fi
    fi

    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}"
        chmod +x "${INSTALL_DIR}/${BINARY}"
    else
        info "Requires elevated permissions..."
        sudo mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY}"
    fi

    success "Installed to ${INSTALL_DIR}/${BINARY}"
}

# ─── PATH Configuration ─────────────────────────────────────────────────────
configure_path() {
    [ "${TUTU_NO_MODIFY_PATH:-0}" = "1" ] && return

    case ":$PATH:" in
        *":${INSTALL_DIR}:"*) return ;;
    esac

    info "Adding ${INSTALL_DIR} to PATH..."

    SHELL_NAME=$(basename "${SHELL:-/bin/sh}")
    case "$SHELL_NAME" in
        zsh)  SHELL_RC="$HOME/.zshrc" ;;
        bash)
            if [ -f "$HOME/.bashrc" ]; then SHELL_RC="$HOME/.bashrc"
            else SHELL_RC="$HOME/.bash_profile"; fi ;;
        fish) SHELL_RC="$HOME/.config/fish/config.fish" ;;
        *)    SHELL_RC="$HOME/.profile" ;;
    esac

    if [ -n "$SHELL_RC" ] && ! grep -q "TUTU" "$SHELL_RC" 2>/dev/null; then
        printf '\n# TuTu — Local-First AI Runtime\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >> "$SHELL_RC"
        info "Updated ${SHELL_RC}"
    fi
}

# ─── TuTu Home Setup ────────────────────────────────────────────────────────
setup_tutu_home() {
    TUTU_HOME="${TUTU_HOME:-$HOME/.tutu}"
    if [ ! -d "$TUTU_HOME" ]; then
        mkdir -p "$TUTU_HOME/bin" "$TUTU_HOME/models" "$TUTU_HOME/keys"
        info "Created TuTu home: ${TUTU_HOME}"
    fi
}

# ─── Optional Systemd Service ───────────────────────────────────────────────
offer_service_install() {
    [ "$INIT_SYSTEM" != "systemd" ] && return
    [ -f "/etc/systemd/system/tutu.service" ] && return
    [ ! -t 0 ] && return

    printf "\n  ${CYAN}Optional:${RESET} Install TuTu as a systemd service? (y/N) "
    read -r answer
    case "$answer" in
        [Yy]*)
            TUTU_USER=$(whoami)
            cat > /tmp/tutu.service << SVCEOF
[Unit]
Description=TuTu — Local-First AI Runtime
After=network.target
Documentation=https://tutuengine.tech/docs.html

[Service]
Type=simple
User=${TUTU_USER}
ExecStart=${INSTALL_DIR}/tutu serve
Restart=on-failure
RestartSec=5
Environment=TUTU_HOME=${TUTU_HOME:-$HOME/.tutu}
LimitNOFILE=65535
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=${TUTU_HOME:-$HOME/.tutu}

[Install]
WantedBy=multi-user.target
SVCEOF
            sudo mv /tmp/tutu.service /etc/systemd/system/tutu.service
            sudo systemctl daemon-reload
            success "Systemd service installed"
            info "Start: sudo systemctl start tutu"
            info "Enable on boot: sudo systemctl enable tutu"
            ;;
    esac
}

# ─── Post-Install Verification (with waiting) ───────────────────────────────
verify_installation() {
    step "Verifying installation..."

    # Wait for filesystem to settle (NFS/slow disks)
    attempt=0
    while [ $attempt -lt 5 ]; do
        if [ -x "${INSTALL_DIR}/${BINARY}" ]; then break; fi
        attempt=$((attempt + 1))
        sleep 1
    done

    INSTALLED_VERSION=""
    if [ -x "${INSTALL_DIR}/${BINARY}" ]; then
        INSTALLED_VERSION=$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null || echo "installed")
    fi

    if [ -n "$INSTALLED_VERSION" ]; then
        success "TuTu ${VERSION} installed successfully! (${INSTALLED_VERSION})"
    else
        warn "Binary installed but could not verify."
        warn "Location: ${INSTALL_DIR}/${BINARY}"
    fi
}

# ─── Build Instructions Fallback ────────────────────────────────────────────
show_build_instructions() {
    printf "\n"
    warn "Pre-built binary not available for ${PLATFORM}/${ARCH} (${VERSION})."
    printf "\n"
    printf "    git clone https://github.com/${REPO}.git\n"
    printf "    cd tutuengine\n"
    printf "    go build -o tutu ./cmd/tutu\n"
    printf "    sudo mv tutu ${INSTALL_DIR:-/usr/local/bin}/tutu\n"
    printf "\n"
    info "Releases: https://github.com/${REPO}/releases"
}

# ─── Success Message ────────────────────────────────────────────────────────
show_success() {
    printf "\n"
    printf "  ${GREEN}${BOLD}═══════════════════════════════════════════${RESET}\n"
    printf "  ${GREEN}${BOLD}  TuTu installed successfully!${RESET}\n"
    printf "  ${GREEN}${BOLD}═══════════════════════════════════════════${RESET}\n"
    printf "\n"
    printf "  ${BOLD}Get started:${RESET}\n"
    printf "    ${CYAN}tutu run llama3.2${RESET}        Chat with Llama 3.2\n"
    printf "    ${CYAN}tutu run phi3${RESET}            Chat with Phi-3\n"
    printf "    ${CYAN}tutu run qwen2.5${RESET}         Chat with Qwen 2.5\n"
    printf "    ${CYAN}tutu serve${RESET}               Start API server (port 11434)\n"
    printf "    ${CYAN}tutu list${RESET}                List downloaded models\n"
    printf "    ${CYAN}tutu --help${RESET}              See all commands\n"
    printf "\n"
    printf "  ${BOLD}API endpoints:${RESET}\n"
    printf "    Ollama:   ${CYAN}http://localhost:11434/api/chat${RESET}\n"
    printf "    OpenAI:   ${CYAN}http://localhost:11434/v1/chat/completions${RESET}\n"
    printf "    MCP:      ${CYAN}http://localhost:11434/mcp${RESET}\n"
    printf "\n"
    printf "  ${BOLD}Docs:${RESET} ${CYAN}https://tutuengine.tech/docs.html${RESET}\n"
    printf "\n"

    case ":$PATH:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            printf "  ${YELLOW}▸ Restart your terminal or run: source ~/.$(basename "${SHELL:-sh}")rc${RESET}\n\n"
            ;;
    esac
}

# ─── Main ────────────────────────────────────────────────────────────────────
main() {
    banner
    detect_platform
    info "Platform: ${PLATFORM}/${ARCH}"
    [ -n "${MACOS_VERSION:-}" ] && info "macOS: ${MACOS_VERSION}"
    check_dependencies
    resolve_version
    check_existing
    download_binary
    install_binary
    configure_path
    setup_tutu_home
    verify_installation
    offer_service_install
    show_success
}

main "$@"
