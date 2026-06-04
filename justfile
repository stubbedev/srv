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

# Format Go code (golangci-lint v2 formatters — gofmt, per .golangci.yml)
fmt:
    golangci-lint fmt ./...

# Run go vet
vet:
    go vet ./...

# Format, vet, then run the linters. The mutating local-dev gate.
lint: fmt
    go vet ./...
    golangci-lint run ./...

# Auto-fix everything mechanically fixable (formatting + golangci --fix).
lint-fix:
    golangci-lint fmt ./...
    golangci-lint run --fix ./...

# Strict read-only check — same logic CI runs, exposed for local pre-push
# verification. Fails if formatting would change or any linter fires.
lint-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(golangci-lint fmt --diff ./...)
    if [ -n "$out" ]; then
        echo "code is not formatted; run 'just fmt':"
        printf '%s\n' "$out"
        exit 1
    fi
    go vet ./...
    golangci-lint run ./...

# Regenerate JSON Schemas under schemas/ from Go structs
schemas:
    #!/usr/bin/env bash
    set -euo pipefail
    go run ./cmd/gen-schema
    if [ -n "$(git status --porcelain schemas/)" ]; then
        echo "schemas: regenerated schemas/"
    else
        echo "schemas: already in sync"
    fi

# Regenerate docs/cli.md from the cobra command tree
sync-docs:
    #!/usr/bin/env bash
    set -euo pipefail
    go run ./cmd/gen-docs
    if [ -n "$(git status --porcelain docs/cli.md)" ]; then
        echo "sync-docs: regenerated docs/cli.md"
    else
        echo "sync-docs: already in sync"
    fi

# Keep flake.nix's `vendorHash` aligned with the current go.sum.
#
# A sha256 of go.sum is embedded as a `# go-sum:` line in flake.nix. When
# the cached digest matches go.sum on disk, sync-flake returns immediately
# without running `nix build`. That makes it cheap enough to run on every
# `just check`, so a dev `go get` flow can never push a master commit that
# breaks the nix CI job. Pass `--force` to bypass the cache.
#
# srv's flake.nix derives `version` from the git rev, so (unlike treeman)
# there is no version string to rewrite here — vendorHash only.
sync-flake force="":
    #!/usr/bin/env bash
    set -euo pipefail
    GO_SUM_HASH=$(sha256sum go.sum | awk '{print $1}')
    CACHED_HASH=$(awk -F': ' '/^[[:space:]]*#[[:space:]]*go-sum:/ {print $2; exit}' flake.nix | tr -d ' ')
    if [ "{{force}}" != "--force" ] && [ "$GO_SUM_HASH" = "$CACHED_HASH" ]; then
        echo "sync-flake: up-to-date (go.sum=$GO_SUM_HASH)"
        exit 0
    fi
    echo "sync-flake: refreshing vendorHash"
    SENTINEL="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    sed -i -E 's|^(\s*vendorHash = )"sha256-[^"]*";|\1"'"$SENTINEL"'";|' flake.nix
    set +e
    OUT=$(nix build .#srv --no-link 2>&1)
    BUILD_STATUS=$?
    set -e
    NEW_HASH=$(printf '%s\n' "$OUT" | awk '/got:[[:space:]]*sha256-/ {print $2; exit}')
    if [ -z "$NEW_HASH" ]; then
        if [ "$BUILD_STATUS" = "0" ]; then
            echo "sync-flake: unexpected nix build success with sentinel hash" >&2
            echo "$OUT" >&2
            exit 1
        fi
        echo "$OUT" >&2
        echo "sync-flake: nix build did not print 'got: sha256-…'" >&2
        exit 1
    fi
    sed -i -E 's|^(\s*vendorHash = )"sha256-[^"]*";|\1"'"$NEW_HASH"'";|' flake.nix
    if grep -q '^[[:space:]]*# go-sum:' flake.nix; then
        sed -i -E 's|^(\s*# go-sum:).*|\1 '"$GO_SUM_HASH"'|' flake.nix
    else
        sed -i -E 's|^(\s*vendorHash = )|            # go-sum: '"$GO_SUM_HASH"'\n\1|' flake.nix
    fi
    echo "sync-flake: vendorHash=$NEW_HASH go-sum=$GO_SUM_HASH"
    # Hard guard: never leave the sentinel behind — CI would fail on hash mismatch.
    if grep -q '^\s*vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="' flake.nix; then
        echo "sync-flake: refusing to leave sentinel vendorHash in flake.nix" >&2
        exit 1
    fi
    nix build .#srv --no-link

# Run all checks — useful for CI parity and pre-push. Mirrors treeman:
# lint (fmt+vet+run), tests, and the generated-artifact sync recipes.
check: lint test schemas sync-docs sync-flake

# Run tests.
# -timeout 60s caps every package so a hung test is visible in seconds
# instead of go's 10-minute default.
test:
    go test -timeout 60s ./...

# Run end-to-end tests (build-tagged `e2e`). Boots a real Traefik via docker
# compose and routes real HTTP through it. Needs docker + mkcert + free ports
# 80/443/88/8080; tests self-skip when those aren't available.
test-e2e:
    go test -tags=e2e -v ./e2e/... -timeout 30m

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
    # `just check` runs lint + tests + schema/docs regen + sync-flake, so any
    # generated-artifact or vendorHash drift is fixed before we tag.
    just check
    if [ -n "$(git status --porcelain)" ]; then
        echo "Changes detected (formatting / generated artifacts / vendorHash). Staging and committing..."
        git add -A
        git commit -m "chore: sync generated artifacts for release"
        echo "Committed"
    fi
    echo "Updating flake.lock..."
    nix flake update
    if [ -n "$(git status --porcelain flake.lock)" ]; then
        git add flake.lock
        git commit -m "chore: update flake.lock for release"
        echo "Committed flake.lock update"
    fi
    echo "Re-validating nix build against the new lock (refreshing vendorHash if stale)..."
    just sync-flake --force
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

