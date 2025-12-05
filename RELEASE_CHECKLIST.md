# Lux Netrunner v1.14.0 Release Checklist

**Date**: 2025-11-12
**Target Version**: v1.14.0
**Current Version**: v1.13.5-lux.3

## Pre-Release Setup

### 1. Documentation Review
- [ ] Read `/Users/z/work/lux/netrunner/RELEASE_ANALYSIS.md`
- [ ] Read `/Users/z/work/lux/netrunner/RELEASE_TESTING.md`
- [ ] Read `/Users/z/work/lux/netrunner/RELEASE_WORKFLOW_SUMMARY.md`
- [ ] Understand workflow changes
- [ ] Review goreleaser.yml changes

### 2. Local Environment
- [ ] Go 1.25+ installed (`go version`)
- [ ] Git configured (`git config --list`)
- [ ] GitHub CLI installed (`gh --version`)
- [ ] GitHub CLI authenticated (`gh auth status`)
- [ ] Working directory clean (`git status`)
- [ ] On main branch (`git checkout main`)
- [ ] Latest changes pulled (`git pull origin main`)

### 3. Optional: Install goreleaser
```bash
brew install goreleaser/tap/goreleaser
goreleaser --version
```

## Phase 1: Local Testing (Optional but Recommended)

### Test Build
```bash
cd /Users/z/work/lux/netrunner

# Snapshot build (no tag required)
goreleaser build --snapshot --clean

# Verify build succeeded
ls -lh dist/
```

**Expected Output**:
```
dist/netrunner_v1.14.0-next_linux_amd64/
dist/netrunner_v1.14.0-next_linux_arm64/
dist/netrunner_v1.14.0-next_darwin_amd64/
dist/netrunner_v1.14.0-next_darwin_arm64/
dist/netrunner_v1.14.0-next_windows_amd64/
```

- [ ] Build completed without errors
- [ ] All 5 platform directories present
- [ ] Binary sizes reasonable (15-20 MB)

### Test Local Binary
```bash
# Test your platform's binary
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner version
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner --help
./dist/netrunner_v1.14.0-next_darwin_arm64/netrunner server --help
```

- [ ] Binary executes without errors
- [ ] Version shows correctly
- [ ] Help commands work
- [ ] No missing libraries

### Test Full Release (Local)
```bash
# Simulate full release
goreleaser release --snapshot --clean --skip-publish

# Verify archives
ls -lh dist/*.tar.gz dist/*.zip
```

**Expected Archives**:
- `netrunner_v1.14.0-next_linux_amd64.tar.gz`
- `netrunner_v1.14.0-next_linux_arm64.tar.gz`
- `netrunner_v1.14.0-next_darwin_amd64.tar.gz`
- `netrunner_v1.14.0-next_darwin_arm64.tar.gz`
- `netrunner_v1.14.0-next_windows_amd64.zip`
- `SHA256SUMS`

- [ ] All 5 archives created
- [ ] SHA256SUMS file present
- [ ] Archives extract successfully
- [ ] Checksums validate: `sha256sum -c dist/SHA256SUMS`

## Phase 2: Release Candidate Testing

### Create RC Tag
```bash
cd /Users/z/work/lux/netrunner

# Ensure clean state
git status
git pull origin main

# Create RC tag
git tag -a v1.14.0-rc.1 -m "Release candidate v1.14.0-rc.1

Testing multi-platform release workflow:
- Windows support
- Enhanced automation
- Comprehensive documentation"

# Push tag (triggers workflow)
git push origin v1.14.0-rc.1
```

- [ ] Tag created locally
- [ ] Tag pushed to GitHub
- [ ] Workflow triggered (check GitHub Actions)

### Monitor Workflow
```bash
# Watch workflow execution
gh run watch

# Or view in browser
gh workflow view release --web
```

**Monitor Progress**:
- [ ] ✅ validate-version job passes (~10 seconds)
- [ ] ✅ test job passes (~40 seconds)
- [ ] ✅ release job passes (~5-10 minutes)

**If any job fails**: Check logs with `gh run view --log`, fix issues, delete tag, recreate.

### Verify RC Release
```bash
# List releases
gh release list

# View RC release
gh release view v1.14.0-rc.1

# Download assets
gh release download v1.14.0-rc.1 --dir ./test-release-rc1
cd test-release-rc1
```

- [ ] Release created on GitHub
- [ ] All 6 assets present (5 binaries + checksums)
- [ ] Release notes formatted correctly
- [ ] Changelog generated properly
- [ ] Marked as pre-release (if RC tag detected)

### Verify Assets
```bash
cd test-release-rc1

# Verify checksums
sha256sum -c SHA256SUMS
# or on macOS
shasum -a 256 -c SHA256SUMS
```

- [ ] All checksums valid
- [ ] File sizes reasonable (15-20 MB compressed)
- [ ] Archive names follow pattern: `netrunner_v1.14.0-rc.1_{os}_{arch}.{ext}`

### Test Downloaded Binaries

**Linux AMD64**:
```bash
tar -xzf netrunner_v1.14.0-rc.1_linux_amd64.tar.gz
./netrunner version
# Expected: netrunner version v1.14.0-rc.1
```

- [ ] Extracts successfully
- [ ] Binary executes
- [ ] Version correct

**macOS (your platform)**:
```bash
tar -xzf netrunner_v1.14.0-rc.1_darwin_arm64.tar.gz
./netrunner version
./netrunner --help
```

- [ ] Extracts successfully
- [ ] Binary executes
- [ ] No Gatekeeper issues (unsigned is expected)
- [ ] Help works

**Windows** (if available or skip):
```bash
unzip netrunner_v1.14.0-rc.1_windows_amd64.zip
./netrunner.exe version  # In WSL or Windows
```

- [ ] Extracts successfully
- [ ] Binary executes (or skipped if no Windows access)

### Integration Test (Optional)
```bash
# Start a test network with RC binary
./netrunner server --port=:8080 --grpc-gateway-port=:8081 &
SERVER_PID=$!

# Basic health check
sleep 2
curl http://localhost:8081/health

# Cleanup
kill $SERVER_PID
```

- [ ] Server starts successfully
- [ ] Health endpoint responds
- [ ] No crashes or errors

### RC Decision
If all checks pass:
- [ ] RC testing successful

If any checks fail:
- [ ] Issues documented
- [ ] Delete RC release: `gh release delete v1.14.0-rc.1 --yes`
- [ ] Delete tags: `git tag -d v1.14.0-rc.1 && git push origin :refs/tags/v1.14.0-rc.1`
- [ ] Fix issues
- [ ] Repeat Phase 2 with v1.14.0-rc.2

### Cleanup RC
```bash
# Delete RC release (once satisfied)
gh release delete v1.14.0-rc.1 --yes

# Delete local tag
git tag -d v1.14.0-rc.1

# Delete remote tag
git push origin :refs/tags/v1.14.0-rc.1

# Cleanup downloaded files
cd ..
rm -rf test-release-rc1
```

- [ ] RC release deleted from GitHub
- [ ] RC tag deleted locally and remotely
- [ ] Test files cleaned up

## Phase 3: Production Release

### Pre-Production Checks
- [ ] All RC tests passed
- [ ] No critical issues found
- [ ] Documentation updated with v1.14.0
- [ ] CHANGELOG.md prepared (if exists)
- [ ] README.md version references updated
- [ ] All changes committed and pushed

### Create Production Tag
```bash
cd /Users/z/work/lux/netrunner

# Final sync
git checkout main
git pull origin main
git status  # Ensure clean

# Create production tag
git tag -a v1.14.0 -m "Release v1.14.0

Multi-platform release with comprehensive automation.

## What's New
- Windows support (amd64)
- Enhanced CI/CD with semantic versioning
- Automated checksums and archives
- Structured changelog generation
- Comprehensive release documentation

## Platforms
- Linux (amd64, arm64)
- macOS (Intel, Apple Silicon)
- Windows (amd64)

## Installation
See https://github.com/luxfi/netrunner/releases/tag/v1.14.0

Full changelog: https://github.com/luxfi/netrunner/compare/v1.13.5-lux.3...v1.14.0"

# Push tag
git push origin v1.14.0
```

- [ ] Production tag created
- [ ] Tag message detailed
- [ ] Tag pushed to GitHub

### Monitor Production Release
```bash
# Watch workflow
gh run watch

# Or view in browser
gh workflow view release --web
```

- [ ] ✅ validate-version passes
- [ ] ✅ test passes
- [ ] ✅ release passes
- [ ] No errors in logs

### Verify Production Release
```bash
# View release
gh release view v1.14.0

# Verify assets
gh release view v1.14.0 --json assets --jq '.assets[].name'
```

**Expected Assets**:
```
netrunner_v1.14.0_linux_amd64.tar.gz
netrunner_v1.14.0_linux_arm64.tar.gz
netrunner_v1.14.0_darwin_amd64.tar.gz
netrunner_v1.14.0_darwin_arm64.tar.gz
netrunner_v1.14.0_windows_amd64.zip
SHA256SUMS
```

- [ ] All 6 assets present
- [ ] Release marked as "Latest"
- [ ] Release notes formatted correctly
- [ ] Changelog populated
- [ ] Download URLs work

### Download and Verify Production
```bash
# Download production release
gh release download v1.14.0 --dir ./release-v1.14.0
cd release-v1.14.0

# Verify checksums
sha256sum -c SHA256SUMS

# Test your platform
tar -xzf netrunner_v1.14.0_darwin_arm64.tar.gz
./netrunner version
```

- [ ] All checksums valid
- [ ] Binary version shows v1.14.0
- [ ] No issues running binary

## Phase 4: Post-Release

### Documentation Updates
- [ ] Update `/Users/z/work/lux/netrunner/README.md` with v1.14.0 install URLs
- [ ] Update `/Users/z/work/lux/netrunner/CLAUDE.md` with release notes
- [ ] Update any version references in docs
- [ ] Commit and push documentation updates

### Announcement (Optional)
- [ ] Draft release announcement
- [ ] Post to Discord/Slack (if applicable)
- [ ] Update project website (if applicable)
- [ ] Tweet/social media (if applicable)

### Monitoring
- [ ] Watch for issue reports
- [ ] Monitor download statistics
- [ ] Check for platform-specific problems
- [ ] Document any unexpected behaviors

### Repository Cleanup
```bash
# Remove local test artifacts
cd /Users/z/work/lux/netrunner
rm -rf dist/
rm -rf release-v1.14.0/
```

- [ ] Local test files removed
- [ ] Working directory clean

## Success Criteria

✅ Release is successful when:
1. Workflow completes without errors
2. All 5 platform binaries built and uploaded
3. SHA256SUMS file present and valid
4. Release marked as "Latest" on GitHub
5. Binaries run on target platforms
6. Version output matches tag
7. No critical issues reported within 24 hours

## Rollback Plan (If Needed)

If critical issue discovered post-release:

1. **Document Issue**:
   ```bash
   # Create issue on GitHub
   gh issue create --title "Critical issue in v1.14.0" --body "..."
   ```

2. **Create Hotfix** (if fixable quickly):
   ```bash
   # Fix issue, commit
   git commit -am "fix: critical issue in v1.14.0"
   git push origin main

   # Create hotfix release
   git tag -a v1.14.1 -m "Hotfix release v1.14.1"
   git push origin v1.14.1
   ```

3. **Mark Release as Broken** (if not fixable):
   ```bash
   # Edit release notes to add warning
   gh release edit v1.14.0 --notes "⚠️ **DO NOT USE** - Critical issue found. Use v1.13.5-lux.3 or wait for v1.14.1"
   ```

4. **Revert to Previous** (worst case):
   ```bash
   # Mark as pre-release (removes "Latest" badge)
   gh release edit v1.14.0 --prerelease

   # Users will see v1.13.5-lux.3 as latest
   ```

## Future Releases

For subsequent releases (v1.15.0, v1.16.0, etc.):

1. ✅ Workflow is now automated - no setup needed
2. ✅ Just create tag and push: `git tag -a v1.15.0 -m "..." && git push origin v1.15.0`
3. ✅ Workflow handles everything automatically
4. ⚠️  Still test RC first if major changes

## Notes

- **First Release**: v1.14.0 is the first automated multi-platform release
- **Breaking Changes**: If introducing breaking changes, document in release notes
- **Platform Support**: Can add more platforms by editing `.goreleaser.yml`
- **Build Time**: Expect 5-10 minutes for full release build
- **Rate Limits**: GitHub has rate limits - wait between RC and production

## Questions/Issues

If problems arise during release:
1. Check workflow logs: `gh run view --log`
2. Review documentation in `/Users/z/work/lux/netrunner/RELEASE_*.md`
3. Test locally with goreleaser snapshot
4. Check goreleaser-cross-action issues
5. Verify GitHub token permissions

## Completed Checklist Summary

When finished, you should have:
- ✅ Tested workflow locally (optional)
- ✅ Tested RC (v1.14.0-rc.1)
- ✅ Verified all platforms
- ✅ Created production release (v1.14.0)
- ✅ Verified production artifacts
- ✅ Updated documentation
- ✅ Monitored for issues

**Congratulations!** 🎉 First multi-platform automated release complete!
