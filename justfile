# justfile for srv
# Run `just` to see all available commands

# Configuration
BINARY := "srv"

# Default recipe - list all available commands
default:
    @just --list

# =============================================================================
# Setup & Dependencies
# =============================================================================

# Check if required tools are installed
check-tools:
    #!/usr/bin/env bash
    command -v go >/dev/null 2>&1 || { echo "go is required but not installed." >&2; exit 1; }
    command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint is required but not installed. See: https://golangci-lint.run/usage/install/" >&2; exit 1; }
    command -v gh >/dev/null 2>&1 || { echo "gh (GitHub CLI) is required but not installed. See: https://cli.github.com/" >&2; exit 1; }
    command -v mkcert >/dev/null 2>&1 || { echo "mkcert is a runtime requirement (brew install mkcert / nix profile install nixpkgs#mkcert)." >&2; exit 1; }
    @echo "All required tools are installed!"

# =============================================================================
# Build Commands
# =============================================================================

# Build the srv binary
build:
    go build -o {{BINARY}} .

# Build with version info embedded
build-release:
    #!/usr/bin/env bash
    VERSION=$(git describe --tags --always --dirty)
    COMMIT=$(git rev-parse --short HEAD)
    BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    go build -ldflags "-X github.com/stubbedev/srv/cmd.Version=$VERSION -X github.com/stubbedev/srv/cmd.Commit=$COMMIT -X github.com/stubbedev/srv/cmd.BuildDate=$BUILD_DATE" -o {{BINARY}} .

# Format Go code
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run golangci-lint on the codebase
lint:
    golangci-lint run ./...

# Regenerate JSON Schemas under schemas/ from Go structs
schemas:
    go run ./cmd/gen-schema

# Regenerate docs/cli.md from the cobra command tree
sync-docs:
    go run ./cmd/gen-docs

# Run all checks (fmt, vet, lint, test) - useful for CI
check: fmt vet lint test

# Run tests.
# -timeout 60s caps every package so a hung test is visible in seconds
# instead of go's 10-minute default.
test:
    go test -timeout 60s ./...

# Run tests with coverage
test-cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report: coverage.html"

# Coverage gate: fail if total statement coverage drops below THRESHOLD.
# Per-package thresholds enforce we don't regress in pkgs we've invested in.
COVERAGE_THRESHOLD := "79"

cover-check:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -covermode=atomic -coverprofile=coverage.out ./... >/dev/null
    TOTAL=$(go tool cover -func=coverage.out | awk '/^total:/ {print $NF}' | sed 's/%//')
    THRESHOLD={{COVERAGE_THRESHOLD}}
    echo "Total coverage: ${TOTAL}% (threshold ${THRESHOLD}%)"
    awk -v t="$TOTAL" -v th="$THRESHOLD" 'BEGIN { if (t+0 < th+0) exit 1 }'
    if [ $? -ne 0 ]; then
        echo "FAIL: coverage ${TOTAL}% below threshold ${THRESHOLD}%" >&2
        exit 1
    fi
    echo "OK"

# Download and tidy Go modules
mod:
    go mod download
    go mod tidy

# Remove build artifacts
clean:
    rm -f {{BINARY}}
    rm -f coverage.out coverage.html

# Install binary to GOPATH/bin
install: build
    go install .
    @echo "Installed to $(go env GOPATH)/bin/{{BINARY}}"

# =============================================================================
# Release Commands
# =============================================================================

# Pre-release checks (clean working directory, on default branch)
_release-checks:
    #!/usr/bin/env bash
    set -euo pipefail
    BRANCH=$(git rev-parse --abbrev-ref HEAD)
    DEFAULT_BRANCH=$(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null | sed 's|^origin/||' || git remote show origin 2>/dev/null | grep 'HEAD branch' | awk '{print $NF}')
    if [ -z "$DEFAULT_BRANCH" ]; then
        DEFAULT_BRANCH="main"
    fi
    if [ "$BRANCH" != "$DEFAULT_BRANCH" ]; then
        echo "Error: Not on default branch '$DEFAULT_BRANCH' (currently on '$BRANCH')." >&2
        exit 1
    fi
    just check
    if [ -n "$(git status --porcelain)" ]; then
        echo "Changes detected from formatting. Staging and committing..."
        git add -A
        git commit -m "chore: format code for release"
        echo "Committed formatting changes"
    fi
    echo "Updating flake.lock..."
    nix flake update
    if [ -n "$(git status --porcelain flake.lock)" ]; then
        git add flake.lock
        git commit -m "chore: update flake.lock for release"
        echo "Committed flake.lock update"
    fi
    echo "Verifying nix build (refreshing vendorHash if stale)..."
    for attempt in 1 2 3; do
        BUILD_LOG=$(mktemp)
        if nix build --no-link .#srv 2>"$BUILD_LOG"; then
            rm "$BUILD_LOG"
            break
        fi
        if ! grep -q 'hash mismatch in fixed-output derivation' "$BUILD_LOG"; then
            cat "$BUILD_LOG" >&2
            rm "$BUILD_LOG"
            exit 1
        fi
        OLD=$(grep 'specified:' "$BUILD_LOG" | head -1 | awk '{print $NF}')
        NEW=$(grep -E '^[[:space:]]*got:' "$BUILD_LOG" | head -1 | awk '{print $NF}')
        rm "$BUILD_LOG"
        if [ -z "$OLD" ] || [ -z "$NEW" ]; then
            echo "Error: could not parse hash mismatch from nix output" >&2
            exit 1
        fi
        echo "Updating vendorHash: $OLD -> $NEW"
        sed -i "s|$OLD|$NEW|" flake.nix
        if [ "$attempt" = "3" ]; then
            echo "Error: nix build still failing after vendorHash updates" >&2
            exit 1
        fi
    done
    if [ -n "$(git status --porcelain flake.nix)" ]; then
        git add flake.nix
        git commit -m "chore: update vendorHash for release"
        echo "Committed vendorHash update"
    fi

# Release a new major version (X.y.z -> X+1.0.0) with GitHub release
release-major: _release-checks
    #!/usr/bin/env bash
    set -euo pipefail
    CURRENT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    NEW_MAJOR=$((MAJOR + 1))
    NEW_VERSION="v${NEW_MAJOR}.0.0"
    echo "Bumping from $CURRENT_TAG to $NEW_VERSION"
    git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
    git push origin HEAD
    git push origin "$NEW_VERSION"
    gh release create "$NEW_VERSION" --generate-notes
    echo "Released $NEW_VERSION"

# Release a new minor version (x.Y.z -> x.Y+1.0) with GitHub release
release-minor: _release-checks
    #!/usr/bin/env bash
    set -euo pipefail
    CURRENT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    NEW_MINOR=$((MINOR + 1))
    NEW_VERSION="v${MAJOR}.${NEW_MINOR}.0"
    echo "Bumping from $CURRENT_TAG to $NEW_VERSION"
    git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
    git push origin HEAD
    git push origin "$NEW_VERSION"
    gh release create "$NEW_VERSION" --generate-notes
    echo "Released $NEW_VERSION"

# Release a new patch version (x.y.Z -> x.y.Z+1) with GitHub release
release-patch: _release-checks
    #!/usr/bin/env bash
    set -euo pipefail
    CURRENT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    NEW_PATCH=$((PATCH + 1))
    NEW_VERSION="v${MAJOR}.${MINOR}.${NEW_PATCH}"
    echo "Bumping from $CURRENT_TAG to $NEW_VERSION"
    git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
    git push origin HEAD
    git push origin "$NEW_VERSION"
    gh release create "$NEW_VERSION" --generate-notes
    echo "Released $NEW_VERSION"

# Preview what versions would be created (dry-run)
release-preview:
    #!/usr/bin/env bash
    CURRENT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    echo "Current: $CURRENT_TAG"
    echo "  major: v$((MAJOR + 1)).0.0"
    echo "  minor: v${MAJOR}.$((MINOR + 1)).0"
    echo "  patch: v${MAJOR}.${MINOR}.$((PATCH + 1))"

