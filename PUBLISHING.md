# Publishing & Release Guide

This document outlines the step-by-step instructions for preparing, testing, and publishing a new release of `px0`.

## 1. Prerequisites

Before cutting a new release, ensure you have:

- Git with push and tag permissions for `px0-ai/px0`.
- Go at the version required by `go.mod` (currently 1.25+); CI reads `go-version-file: go.mod`.
- Node.js (v18+ or v20+) or Bun for bundling frontend assets.
- A clean working tree with all tests passing.

## 2. Pre-Release Verification

Run the test suite and verify asset bundling:

```bash
# 1. Run unit and integration tests
make test

# 2. Build local binary and verify sanity
make build
./px0 --version
```

## 3. Release Methods
### Method 1: Automated Release via Makefile (Recommended)

The project includes an automated `make publish` target in the [Makefile](Makefile) that:

1. Updates the [VERSION](VERSION) file.
1. Bundles web assets and cross-compiles binaries into `dist/`.
1. Commits `VERSION`.
1. Creates an annotated Git tag `v<version>`.

#### Step 1: Run publish command

Specify the version without a leading `v`:

```bash
make publish 0.2.0
```

#### Step 2: Push commit and tags to GitHub

```bash
git push origin master --tags
```

### Method 2: Manual Release via Git

```bash
# 1. Update VERSION file
echo "0.2.0" > VERSION

# 2. Bundle frontend assets and compile release binaries
make dist

# 3. Commit version bump
git add VERSION
git commit -m "Release v0.2.0"

# 4. Tag the release
git tag -fa v0.2.0 -m "Release v0.2.0"

# 5. Push to GitHub
git push origin master
git push origin v0.2.0
```

### Method 3: GitHub Actions Workflow Dispatch

1. Navigate to Actions -> Release on GitHub: `https://github.com/px0-ai/px0/actions/workflows/release.yml`
1. Click Run workflow.
1. Enter the version tag (e.g. `v0.2.0`).
1. Trigger the workflow.

## 4. Post-Release Verification

1. Verify GitHub Actions workflow completion on the Actions tab.
1. Confirm artifacts on the [Releases](https://github.com/px0-ai/px0/releases) page (cross-platform binaries and `checksums.txt`). The self-updater requires this file and verifies the selected binary against it before execution.
1. Verify the installer script:
  ```bash
  curl -fsSL https://px0.ai/install.sh | sh
  ```
1. Verify self-update functionality:
  ```bash
  px0 --update
  ```


## Native macOS release

`make desktop` builds a universal Wails `.app`, sets bundle versions, and applies an ad-hoc signature for local use. `make dmg` packages it; these targets require Wails and `create-dmg` respectively, plus Apple's command-line tools/SDK. The desktop build links Cocoa, WebKit, and UniformTypeIdentifiers. The existing GitHub release workflow ships CLI binaries only.

Before a public desktop release, use a Developer ID signature and notarization, then verify supported macOS versions, first launch/folder selection, repeated workspace replacement, shutdown during LSP/install/harness work, fonts/icons, scrolling/caret/selection, and large tree/diff pages. The optional WebKit high-refresh selector is private API and guarded for availability; it is not a cross-version FPS guarantee.

Default Go tests exclude `desktop` files. Obtain approval for exact test commands under AGENTS.md; CLI and native compilation are separate checks. Browser/E2E and performance measurements also require explicit test approval. A successful `go build -tags 'desktop,production'` verifies native compilation but does not replace `.app` signing, runtime, or browser validation.
