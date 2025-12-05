# Lux Netrunner Release - Quick Start Guide

**TL;DR**: Create tag → Push tag → Done! 🚀

## The Shortest Path to Release

```bash
# 1. Test locally (optional but recommended)
goreleaser build --snapshot --clean
./dist/netrunner_*/netrunner version

# 2. Create RC tag
git tag -a v1.14.0-rc.1 -m "RC for v1.14.0"
git push origin v1.14.0-rc.1

# 3. Watch workflow (open in browser)
gh workflow view release --web

# 4. Download and test
gh release download v1.14.0-rc.1
sha256sum -c SHA256SUMS
./netrunner version

# 5. If good, clean up RC
gh release delete v1.14.0-rc.1 --yes
git tag -d v1.14.0-rc.1
git push origin :refs/tags/v1.14.0-rc.1

# 6. Create production release
git tag -a v1.14.0 -m "Release v1.14.0"
git push origin v1.14.0

# 7. Verify
gh release view v1.14.0
```

## What You Get

### Platforms (5 total)
- ✅ Linux amd64
- ✅ Linux arm64
- ✅ macOS Intel (amd64)
- ✅ macOS Apple Silicon (arm64)
- ✅ Windows amd64

### Artifacts
```
netrunner_v1.14.0_linux_amd64.tar.gz
netrunner_v1.14.0_linux_arm64.tar.gz
netrunner_v1.14.0_darwin_amd64.tar.gz
netrunner_v1.14.0_darwin_arm64.tar.gz
netrunner_v1.14.0_windows_amd64.zip
SHA256SUMS
```

### Features
- ✅ Automatic checksums (SHA256)
- ✅ Structured changelog (feat/fix/perf)
- ✅ Release notes with download instructions
- ✅ Semantic version validation
- ✅ Test suite runs before release
- ✅ Cross-platform compilation

## Workflow Jobs

```
validate-version (10s)
   ↓
test (40s)
   ↓
release (5-10min)
```

**Total Time**: ~6-11 minutes

## Version Rules

✅ **Valid**:
- `v1.14.0`
- `v1.14.0-rc.1`
- `v1.14.0-alpha.2`
- `v1.14.0+build.123`

❌ **Invalid**:
- `1.14.0` (missing 'v')
- `v1.14` (incomplete semver)
- `v2.0.0` (v2+ requires module path change)
- `release-1.14.0` (not semver)

## Common Commands

```bash
# Local test (no tag needed)
goreleaser build --snapshot --clean

# Create tag
git tag -a v1.14.0 -m "Release v1.14.0"

# Push tag (triggers release)
git push origin v1.14.0

# Watch workflow
gh run watch

# View release
gh release view v1.14.0

# Download release
gh release download v1.14.0

# Delete release
gh release delete v1.14.0 --yes

# Delete tag
git tag -d v1.14.0
git push origin :refs/tags/v1.14.0
```

## When Things Go Wrong

### Workflow fails validation
**Why**: Tag format invalid
**Fix**: Delete tag, recreate with correct format

### Tests fail
**Why**: Code has failing tests
**Fix**: Fix tests, push, recreate tag

### Build fails
**Why**: Cross-compilation error
**Fix**: Check workflow logs, may need goreleaser.yml adjustment

### Binary doesn't run
**Why**: Missing dependencies or wrong platform
**Fix**: Test on actual hardware, check CGO settings

## Pro Tips

1. **Always test RC first**: `v1.14.0-rc.1` before `v1.14.0`
2. **Use goreleaser locally**: Catch issues before pushing
3. **Keep changelog clean**: Use conventional commits (feat/fix/docs)
4. **Monitor workflow**: Don't walk away after push
5. **Verify checksums**: Always `sha256sum -c SHA256SUMS`

## Files to Know

- `.goreleaser.yml` - Build configuration
- `.github/workflows/release.yml` - Workflow automation
- `RELEASE_ANALYSIS.md` - Detailed architecture
- `RELEASE_TESTING.md` - Comprehensive testing guide
- `RELEASE_CHECKLIST.md` - Step-by-step checklist

## Next Release (Future)

For v1.15.0 and beyond:
```bash
git tag -a v1.15.0 -m "Release v1.15.0"
git push origin v1.15.0
# That's it! 🎉
```

Everything else is automated.

## Install goreleaser (One-Time)

```bash
# macOS/Linux
brew install goreleaser/tap/goreleaser

# Or via Go
go install github.com/goreleaser/goreleaser@latest

# Verify
goreleaser --version
```

## Expected Timeline

| Phase | Time |
|-------|------|
| Local test (optional) | 2 min |
| RC creation | 1 min |
| RC workflow | 6-11 min |
| RC testing | 5 min |
| RC cleanup | 1 min |
| Production creation | 1 min |
| Production workflow | 6-11 min |
| Production testing | 3 min |
| **Total** | **25-41 minutes** |

Without RC testing: **7-12 minutes**

## Help

Detailed help available in:
- `RELEASE_CHECKLIST.md` - Full checklist
- `RELEASE_TESTING.md` - Testing scenarios
- `RELEASE_ANALYSIS.md` - Architecture details

Quick help:
```bash
# Workflow status
gh run list --workflow=release.yml

# View logs
gh run view --log

# Release list
gh release list

# Asset list
gh release view v1.14.0 --json assets
```

## Summary

1. ✅ Workflow is fully automated
2. ✅ Push tag → Get release
3. ✅ 5 platforms supported
4. ✅ Checksums included
5. ✅ Changelog generated
6. ✅ Tests run automatically
7. ✅ Version validated
8. ✅ Ready for v1.14.0! 🚀
