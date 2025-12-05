# Lux Netrunner Release Workflow Analysis

**Date**: 2025-11-12
**Current Version**: v1.13.5-lux.3
**Repository**: luxfi/netrunner

## Current State Analysis

### Existing Workflow (`.github/workflows/release.yml`)
- ✅ Triggers on tag push (`tags: ["*"]`)
- ✅ Uses GoReleaser for builds
- ✅ Supports Linux amd64/arm64, Darwin amd64/arm64
- ❌ **Missing Windows support**
- ❌ **Old Go version** (1.19 vs project requires 1.25.4)
- ❌ **Complex osxcross setup** (may fail, outdated SDK)
- ❌ **No semantic version validation**
- ❌ **No changelog generation**
- ⚠️  **gh CLI error**: Tried to query ava-labs/netrunner instead of luxfi/netrunner

### Existing GoReleaser Config (`.goreleaser.yml`)
- ✅ Supports Linux/Darwin amd64/arm64
- ✅ CGO enabled with portable BLST
- ✅ Cross-compilation setup for ARM64
- ✅ Version injection via ldflags
- ❌ **No Windows in config**
- ❌ **No archive configuration** (defaults may not match requirements)
- ❌ **No checksum configuration**
- ❌ **osxcross clang references** (oa64-clang, o64-clang) may not work in GitHub Actions

### Issues Identified

1. **Go Version Mismatch**: Workflow uses Go 1.19, project requires 1.25.4
2. **Windows Not Supported**: goreleaser.yml doesn't include Windows targets
3. **osxcross Complexity**: Manual SDK download/build (30+ min), fragile
4. **No Version Validation**: Doesn't prevent v2.x.x tags (Go module breaking change)
5. **gh CLI Misconfiguration**: Queried wrong repo (ava-labs vs luxfi)
6. **No Releases Exist**: Despite having tags, no GitHub releases created

## Recommended Solution

### Option 1: Enhanced GoReleaser (RECOMMENDED)
**Pros**:
- Leverages existing `.goreleaser.yml`
- Automatic archive creation, checksums, changelog
- Built-in GitHub release creation
- Industry standard tool
- Minimal workflow maintenance

**Cons**:
- CGO cross-compilation for Darwin is complex
- May need goreleaser-cross Docker image for full cross-compilation

**Implementation**:
- Update `.goreleaser.yml` to include Windows
- Use `goreleaser/goreleaser-cross-action` for CGO cross-compilation
- Add semantic version validation step
- Configure proper archive formats and checksums

### Option 2: Manual Build Matrix
**Pros**:
- Full control over build process
- No external tool dependencies
- Easier debugging

**Cons**:
- More YAML to maintain
- Manual archive creation
- Manual checksum generation
- No automatic changelog

## Proposed Changes

### 1. Update `.goreleaser.yml`
```yaml
builds:
  - id: netrunner
    main: ./main.go
    binary: netrunner
    flags:
      - -v
    ldflags:
      - -X 'github.com/luxfi/netrunner/cmd.Version={{.Version}}'
    goos:
      - linux
      - darwin
      - windows  # ADD WINDOWS
    goarch:
      - amd64
      - arm64
    env:
      - CGO_ENABLED=1
      - CGO_CFLAGS=-O -D__BLST_PORTABLE__
    overrides:
      - goos: linux
        goarch: arm64
        env:
          - CC=aarch64-linux-gnu-gcc
      - goos: darwin
        goarch: arm64
        goarm: 8
      - goos: darwin
        goarch: amd64
        goamd64: v1
      - goos: windows  # WINDOWS OVERRIDE
        goarch: amd64
        env:
          - CC=x86_64-w64-mingw32-gcc
      - goos: windows
        goarch: arm64
        env:
          - CC=aarch64-w64-mingw32-gcc
    ignore:
      - goos: windows
        goarch: arm64  # Skip Windows ARM64 if no cross-compiler

archives:
  - id: netrunner
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: "netrunner_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md

checksum:
  name_template: 'SHA256SUMS'
  algorithm: sha256

release:
  github:
    owner: luxfi
    name: netrunner
  draft: false
  prerelease: auto
  mode: append
  header: |
    ## Lux Netrunner {{ .Tag }}

    Network orchestration and testing framework for Lux blockchain.
  footer: |
    **Full Changelog**: https://github.com/luxfi/netrunner/compare/{{ .PreviousTag }}...{{ .Tag }}
```

### 2. Update `.github/workflows/release.yml`
```yaml
name: Release

on:
  push:
    tags:
      - "v*.*.*"

permissions:
  contents: write

jobs:
  validate-version:
    runs-on: ubuntu-latest
    steps:
      - name: Validate Semantic Version
        run: |
          TAG="${GITHUB_REF#refs/tags/}"
          echo "Validating tag: $TAG"

          # Must start with v
          if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
            echo "Error: Tag must follow semantic versioning (v1.2.3 or v1.2.3-suffix)"
            exit 1
          fi

          # Extract major version
          MAJOR=$(echo "$TAG" | sed -E 's/^v([0-9]+)\..*/\1/')

          # Prevent v2.x.x+ (Go module breaking change)
          if [ "$MAJOR" -ge 2 ]; then
            echo "Error: Major version must be < 2 (Go module constraint)"
            echo "For v2+, update go.mod to github.com/luxfi/netrunner/v2"
            exit 1
          fi

          echo "Version validation passed: $TAG"

  release:
    needs: validate-version
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Run Tests
        run: go test -v ./...

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-cross-action@v4
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Testing the Workflow

### Local Testing (requires goreleaser)
```bash
# Install goreleaser
brew install goreleaser/tap/goreleaser  # macOS
# or
go install github.com/goreleaser/goreleaser@latest

# Test build without releasing
cd /Users/z/work/lux/netrunner
goreleaser build --snapshot --clean

# Test full release process (no push)
goreleaser release --snapshot --clean --skip-publish
```

### Dry Run on GitHub
```bash
# Create a test tag
git tag -a v1.14.0-rc.1 -m "Release candidate for testing"
git push origin v1.14.0-rc.1

# Monitor workflow at:
# https://github.com/luxfi/netrunner/actions

# Delete test release if successful
gh release delete v1.14.0-rc.1 --yes
git tag -d v1.14.0-rc.1
git push origin :refs/tags/v1.14.0-rc.1
```

## Recommended First Release

### Version: v1.14.0

**Rationale**:
- Current: v1.13.5-lux.3
- Next minor: v1.14.0
- Clean version (no suffix) for major release
- Includes all recent improvements and fixes

**Pre-Release Checklist**:
- [ ] Merge all pending changes
- [ ] Update CHANGELOG.md
- [ ] Run full test suite
- [ ] Test build locally with goreleaser
- [ ] Update documentation with new version
- [ ] Create tag: `git tag -a v1.14.0 -m "Release v1.14.0"`
- [ ] Push tag: `git push origin v1.14.0`
- [ ] Verify GitHub release created
- [ ] Test downloading and running binaries

## Migration Plan

### Phase 1: Update Configuration (This Session)
1. ✅ Analyze current workflow
2. ⏳ Update `.goreleaser.yml` with Windows support
3. ⏳ Update `.github/workflows/release.yml` with new workflow
4. ⏳ Test locally with goreleaser --snapshot

### Phase 2: Test Release (Next)
1. Create release candidate tag (v1.14.0-rc.1)
2. Verify all platforms build successfully
3. Download and test binaries on each platform
4. Fix any issues found
5. Delete RC release and tag

### Phase 3: Production Release
1. Create v1.14.0 tag
2. Monitor workflow execution
3. Verify release artifacts
4. Update documentation
5. Announce release

## Cross-Compilation Notes

### CGO Challenges
**Problem**: CGO requires platform-specific compilers
- Linux ARM64: `aarch64-linux-gnu-gcc`
- Darwin ARM64: Native on arm64 runner or osxcross
- Darwin AMD64: Native on amd64 runner or osxcross
- Windows AMD64: `x86_64-w64-mingw32-gcc`

**Solution**: Use `goreleaser/goreleaser-cross-action`
- Provides pre-configured cross-compilation environment
- Includes all necessary toolchains
- Handles osxcross complexity
- Supports CGO for all platforms

### Alternative: Native Runners
For maximum reliability, use platform-specific runners:

```yaml
strategy:
  matrix:
    include:
      - os: ubuntu-latest
        goos: linux
        goarch: amd64
      - os: ubuntu-latest
        goos: linux
        goarch: arm64
      - os: macos-latest
        goos: darwin
        goarch: amd64
      - os: macos-14  # M1 runner
        goos: darwin
        goarch: arm64
      - os: windows-latest
        goos: windows
        goarch: amd64
```

**Trade-offs**:
- More workflow complexity
- Longer total runtime (sequential builds)
- Higher GitHub Actions minutes usage
- But: No cross-compilation issues

## Platform-Specific Considerations

### Linux
- ✅ Easy cross-compilation with apt packages
- ✅ Static binaries possible
- Target: glibc 2.17+ (CentOS 7+)

### macOS
- ⚠️  osxcross is complex but works
- ⚠️  SDK licensing (GitHub has enterprise license)
- Alternative: Use native macOS runners
- Target: macOS 11+ (Intel and Apple Silicon)

### Windows
- ⚠️  CGO requires mingw-w64 toolchain
- ⚠️  Limited testing (no Windows VM in project)
- Consider: Windows CGO may not be required
- Target: Windows 10+ (64-bit)

### Can We Disable CGO for Windows?
**Investigation needed**:
```bash
# Try building without CGO on Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o netrunner.exe
```

If successful, simplify goreleaser.yml:
```yaml
- goos: windows
  goarch: amd64
  env:
    - CGO_ENABLED=0  # Disable CGO for Windows
```

## Success Criteria

### Workflow Must:
1. ✅ Build for all 5 platforms (Linux amd64/arm64, Darwin amd64/arm64, Windows amd64)
2. ✅ Create archives (.tar.gz for Unix, .zip for Windows)
3. ✅ Generate SHA256SUMS file
4. ✅ Create GitHub release automatically
5. ✅ Include changelog
6. ✅ Mark latest release
7. ✅ Validate semantic versioning
8. ✅ Prevent v2.x.x releases without module path update

### Binaries Must:
1. ✅ Run on target platforms
2. ✅ Show correct version (`netrunner version`)
3. ✅ Be reproducible (same input = same output)
4. ✅ Include proper licensing info

## Next Steps

1. **Update `.goreleaser.yml`** with Windows support and proper configuration
2. **Update `.github/workflows/release.yml`** with improved workflow
3. **Test locally** with `goreleaser build --snapshot --clean`
4. **Create RC tag** for workflow testing
5. **Validate binaries** on all platforms
6. **Create v1.14.0 release**

## References

- GoReleaser Docs: https://goreleaser.com/
- goreleaser-cross: https://github.com/goreleaser/goreleaser-cross
- GitHub Actions: https://docs.github.com/en/actions
- Semantic Versioning: https://semver.org/
- Go Modules: https://go.dev/ref/mod#major-version-suffixes
