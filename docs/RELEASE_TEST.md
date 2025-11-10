# Release E2E Test Plan

This document describes how to execute an end-to-end (E2E) test of the release pipeline using a test tag and how to validate artifacts and checksums.

## 1) Local Dry Run (no publish)

Use GoReleaser snapshot mode to quickly validate builds and packaging without creating a GitHub Release.

```bash
# From repository root
goreleaser release --snapshot --skip-publish --clean

# Inspect artifacts
ls -la dist/
```

Verify that the following artifacts are created for both linux/amd64 and linux/arm64:

- tar.gz archives: `ragent_<VERSION>_linux_amd64.tar.gz`, `ragent_<VERSION>_linux_arm64.tar.gz`
- checksums file: `checksums_<VERSION>.txt`

Verify checksums locally:

```bash
cd dist
sha256sum -c checksums_*.txt
# or on macOS
shasum -a 256 -c checksums_*.txt
```

## 2) GitHub Actions E2E (test tag)

Run a full release on GitHub using a test tag. This will create a test Release, build binaries for both architectures, upload archives and checksums, and generate release notes from git history.

```bash
ts=$(date +%Y%m%d%H%M)
export TEST_TAG="v0.0.1-test-${ts}"

git tag "${TEST_TAG}"
git push origin "${TEST_TAG}"

echo "Pushed ${TEST_TAG}. Monitor progress: https://github.com/ca-srg/ragent/actions?query=workflow%3ARelease+event%3Apush+branch%3A${TEST_TAG}"
```

### Verify in GitHub

1. Actions → Release: confirm the workflow starts and completes in under 5 minutes.
2. Releases page: confirm a new Release for `${TEST_TAG}` exists with:
   - tar.gz archives for linux/amd64 and linux/arm64
   - checksums file (SHA256)
   - release notes generated from git changelog (doc/test commits excluded)
3. Download artifacts locally and verify checksums:

```bash
# From the downloaded files directory
sha256sum -c checksums_${TEST_TAG#v}.txt || true
shasum -a 256 -c checksums_${TEST_TAG#v}.txt || true
```

4. Execute the binaries:

```bash
chmod +x ragent
./ragent --help
./ragent --version || true  # the binary may not embed version
```

## 3) Cleanup (test release)

Remove the test tag locally and on the remote. Optionally delete the Release via GitHub UI or `gh` CLI.

```bash
# Delete local tag
git tag -d "${TEST_TAG}"

# Delete remote tag
git push origin :refs/tags/"${TEST_TAG}"

# Optional: delete the GitHub Release (requires permissions)
# gh release delete "${TEST_TAG}" -y
# or delete via GitHub Releases UI
```

## Success Criteria

- Workflow triggered successfully on `${TEST_TAG}` push
- Builds finished under 5 minutes
- linux/amd64 and linux/arm64 artifacts present
- checksums verified successfully
- binaries run and show help/version output
- test tag and release cleaned up

