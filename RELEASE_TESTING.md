# Release Workflow Testing Guide

**Project**: Lux Netrunner
**Repository**: luxfi/netrunner
**Date**: 2025-11-12

## Prerequisites

### Local Testing (Optional but Recommended)

Install goreleaser for local testing:

```bash
# macOS
brew install goreleaser/tap/goreleaser

# Linux
brew install goreleaser/tap/goreleaser
# or
go install github.com/goreleaser/goreleaser@latest

# Verify installation
goreleaser --version
```

### GitHub Setup

Ensure you have:
- Push access to luxfi/netrunner repository
- GitHub CLI installed (`brew install gh` or see https://cli.github.com/)
- Authenticated with `gh auth login`

## Testing Workflow Locally

### 1. Test Build Without Release

```bash
cd /Users/z/work/lux/netrunner

# Snapshot build (no git tags required)
goreleaser build --snapshot --clean

# Output will be in ./dist/
ls -lh dist/
```

Expected output:
```
netrunner_v1.14.0-next_linux_amd64/
netrunner_v1.14.0-next_linux_arm64/
netrunner_v1.14.0-next_darwin_amd64/
netrunner_v1.14.0-next_darwin_arm64/
netrunner_v1.14.0-next_windows_amd64/
```

### 2. Test Full Release Process (Local)

```bash
# Simulate full release without publishing
goreleaser release --snapshot --clean --skip-publish

# Check generated artifacts
ls -lh dist/
```

Expected artifacts:
- `netrunner_v1.14.0-next_linux_amd64.tar.gz`
- `netrunner_v1.14.0-next_linux_arm64.tar.gz`
- `netrunner_v1.14.0-next_darwin_amd64.tar.gz`
- `netrunner_v1.14.0-next_darwin_arm64.tar.gz`
- `netrunner_v1.14.0-next_windows_amd64.zip`
- `SHA256SUMS`

### 3. Test Binary Functionality

```bash
# Test current platform binary (example for macOS arm64)
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner version

# Expected output:
# netrunner version v1.14.0-next

# Test help command
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner --help

# Test server start (quick test)
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner server --help
```

### 4. Verify Checksums

```bash
cd dist/

# Verify all checksums
sha256sum -c SHA256SUMS

# Or verify specific file
sha256sum netrunner_v1.14.0-next_linux_amd64.tar.gz
grep "netrunner_v1.14.0-next_linux_amd64.tar.gz" SHA256SUMS
```

## Testing on GitHub (Release Candidate)

### Step 1: Create RC Tag

```bash
cd /Users/z/work/lux/netrunner

# Ensure you're on latest main
git checkout main
git pull origin main

# Create release candidate tag
git tag -a v1.14.0-rc.1 -m "Release candidate v1.14.0-rc.1 for testing"

# Push tag to trigger workflow
git push origin v1.14.0-rc.1
```

### Step 2: Monitor Workflow

```bash
# Watch workflow execution
gh workflow view release

# Or open in browser
gh workflow view release --web

# Check run status
gh run list --workflow=release.yml --limit 1

# View logs if needed
gh run view --log
```

### Step 3: Verify Release

```bash
# List releases
gh release list

# View specific release
gh release view v1.14.0-rc.1

# Download all assets
gh release download v1.14.0-rc.1 --dir ./test-release

# Verify checksums
cd test-release
sha256sum -c SHA256SUMS
```

### Step 4: Test Downloaded Binaries

```bash
cd test-release

# Extract Linux binary
tar -xzf netrunner_v1.14.0-rc.1_linux_amd64.tar.gz
./netrunner version

# Extract macOS binary (if on macOS)
tar -xzf netrunner_v1.14.0-rc.1_darwin_arm64.tar.gz
./netrunner version

# Extract Windows binary (if on Windows or WSL)
unzip netrunner_v1.14.0-rc.1_windows_amd64.zip
./netrunner.exe version
```

### Step 5: Clean Up RC Release

If testing successful:

```bash
# Delete release
gh release delete v1.14.0-rc.1 --yes

# Delete local tag
git tag -d v1.14.0-rc.1

# Delete remote tag
git push origin :refs/tags/v1.14.0-rc.1

# Clean up downloaded files
rm -rf test-release
```

## Production Release Process

### Pre-Release Checklist

- [ ] All tests passing locally (`go test ./...`)
- [ ] All tests passing on CI
- [ ] CHANGELOG.md updated
- [ ] Version number decided (v1.14.0)
- [ ] RC testing completed successfully
- [ ] Documentation updated with new version

### Create Production Release

```bash
cd /Users/z/work/lux/netrunner

# Ensure clean working directory
git status

# Ensure on latest main
git checkout main
git pull origin main

# Create production tag
git tag -a v1.14.0 -m "Release v1.14.0

Features:
- Windows support added
- Enhanced cross-platform builds
- Improved release automation

See CHANGELOG.md for full details."

# Push tag (this triggers release workflow)
git push origin v1.14.0
```

### Monitor Release

```bash
# Watch workflow
gh run watch

# Or view in browser
gh workflow view release --web
```

### Verify Production Release

```bash
# View release
gh release view v1.14.0

# Verify all assets present
gh release view v1.14.0 --json assets --jq '.assets[].name'
```

Expected assets:
- `netrunner_v1.14.0_linux_amd64.tar.gz`
- `netrunner_v1.14.0_linux_arm64.tar.gz`
- `netrunner_v1.14.0_darwin_amd64.tar.gz`
- `netrunner_v1.14.0_darwin_arm64.tar.gz`
- `netrunner_v1.14.0_windows_amd64.zip`
- `SHA256SUMS`

### Post-Release Checklist

- [ ] Release appears on GitHub releases page
- [ ] All 5 platform binaries present
- [ ] SHA256SUMS file present
- [ ] Release notes generated correctly
- [ ] Changelog formatted properly
- [ ] Download links work
- [ ] Checksums verify correctly
- [ ] Binaries run on target platforms

## Platform-Specific Testing

### Linux (amd64)

```bash
# Download and test
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/netrunner_v1.14.0_linux_amd64.tar.gz
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf netrunner_v1.14.0_linux_amd64.tar.gz
./netrunner version
./netrunner --help
```

### Linux (arm64)

```bash
# On ARM64 system or emulator
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/netrunner_v1.14.0_linux_arm64.tar.gz
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf netrunner_v1.14.0_linux_arm64.tar.gz
./netrunner version
```

### macOS (Intel)

```bash
# On Intel Mac
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/netrunner_v1.14.0_darwin_amd64.tar.gz
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf netrunner_v1.14.0_darwin_amd64.tar.gz
./netrunner version
```

### macOS (Apple Silicon)

```bash
# On M1/M2/M3 Mac
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/netrunner_v1.14.0_darwin_arm64.tar.gz
curl -LO https://github.com/luxfi/netrunner/releases/download/v1.14.0/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf netrunner_v1.14.0_darwin_arm64.tar.gz
./netrunner version
```

### Windows (amd64)

```powershell
# PowerShell
Invoke-WebRequest -Uri https://github.com/luxfi/netrunner/releases/download/v1.14.0/netrunner_v1.14.0_windows_amd64.zip -OutFile netrunner.zip
Invoke-WebRequest -Uri https://github.com/luxfi/netrunner/releases/download/v1.14.0/SHA256SUMS -OutFile SHA256SUMS
Expand-Archive netrunner.zip
.\netrunner\netrunner.exe version
```

## Troubleshooting

### Issue: Workflow fails at validation step

**Cause**: Tag doesn't match semantic versioning pattern

**Solution**: Delete tag and recreate with proper format
```bash
git tag -d v1.14.0-invalid
git push origin :refs/tags/v1.14.0-invalid
git tag -a v1.14.0 -m "Release v1.14.0"
git push origin v1.14.0
```

### Issue: Workflow fails at test step

**Cause**: Tests failing

**Solution**: Fix tests before creating tag
```bash
go test ./...  # Run locally first
git commit -am "fix: resolve test failures"
git push origin main
git tag -a v1.14.0 -m "Release v1.14.0"
git push origin v1.14.0
```

### Issue: GoReleaser fails on cross-compilation

**Cause**: Missing cross-compiler or CGO issue

**Solution**: Check goreleaser-cross-action logs, may need to disable CGO for problematic platform

### Issue: Binary doesn't run on target platform

**Cause**: Missing dynamic libraries or incompatible build

**Solution**:
- Verify CGO_ENABLED setting
- Check ldflags for static linking
- Test on actual target platform, not emulator

### Issue: Checksums don't match

**Cause**: File corruption during download or generation

**Solution**: Re-download from GitHub release, verify no proxy/CDN caching issues

## Success Criteria

A release is successful when:

1. ✅ **Workflow completes** without errors
2. ✅ **All platforms built** (5 binaries present)
3. ✅ **Release created** on GitHub
4. ✅ **Checksums valid** for all artifacts
5. ✅ **Binaries run** on target platforms
6. ✅ **Version correct** in binary output
7. ✅ **Archives extract** without errors
8. ✅ **Changelog generated** properly
9. ✅ **Release marked latest** (if not pre-release)
10. ✅ **Download links work** from GitHub

## Automation Improvements (Future)

Consider adding:

1. **Binary smoke tests** in workflow (run `--version` on each platform)
2. **Automated platform testing** via matrix of runners
3. **Integration test** execution before release
4. **Docker image** builds alongside binaries
5. **Homebrew tap** update automation
6. **Announcement posting** to Discord/Slack
7. **Documentation versioning** automation

## Useful Commands Reference

```bash
# Local testing
goreleaser build --snapshot --clean
goreleaser release --snapshot --clean --skip-publish

# Tag management
git tag -a v1.14.0 -m "Release v1.14.0"
git push origin v1.14.0
git tag -d v1.14.0
git push origin :refs/tags/v1.14.0

# GitHub CLI
gh release list
gh release view v1.14.0
gh release download v1.14.0
gh release delete v1.14.0 --yes

# Workflow monitoring
gh workflow list
gh workflow view release
gh run list --workflow=release.yml
gh run watch

# Checksum verification
sha256sum -c SHA256SUMS
sha256sum netrunner_v1.14.0_linux_amd64.tar.gz
shasum -a 256 -c SHA256SUMS  # macOS
```

## Next Steps After First Release

1. Update installation documentation with release URLs
2. Test installation instructions on clean systems
3. Gather feedback on binary compatibility
4. Monitor issue tracker for platform-specific problems
5. Document any platform-specific quirks discovered
6. Plan next release with improvements

## References

- GoReleaser Docs: https://goreleaser.com/
- GitHub Actions Docs: https://docs.github.com/en/actions
- GitHub CLI Manual: https://cli.github.com/manual/
- Semantic Versioning: https://semver.org/
