# GitHub Data Source Integration

## TL;DR

> **Quick Summary**: Add GitHub repositories as a new data source for RAGent's vectorize pipeline. Clone/fetch repos via go-git, recursively scan for .md/.csv files, and auto-generate metadata (owner, repo name, directory-based category) before vectorization.
> 
> **Deliverables**:
> - `internal/scanner/github_scanner.go` — GitHub repository scanner
> - `internal/scanner/github_scanner_test.go` — Unit tests
> - Modified `cmd/vectorize.go` — New `--github-repos` flag + GitHub source integration
> - Modified `internal/metadata/extractor.go` — GitHub-specific metadata extraction
> - Modified `internal/types/types.go` — GitHub config fields
> - Updated documentation (README.md, README_ja.md)
> 
> **Estimated Effort**: Medium
> **Parallel Execution**: YES - 2 waves
> **Critical Path**: Task 1 (types) → Task 2 (scanner) → Task 3 (metadata) → Task 4 (vectorize integration) → Task 5 (tests) → Task 6 (docs)

---

## Context

### Original Request
Add GitHub as a new data source to RAGent. Clone/fetch repositories periodically, recursively find .md/.csv files, and vectorize them. For GitHub-sourced files without frontmatter metadata, auto-set metadata from the repository structure: owner name, repository name, and parent directory names as categories.

### Interview Summary
**Key Discussions**:
- **Vectorize integration**: GitHub source integrated into existing `vectorize` command, including `--follow` mode support
- **Multiple repos**: Comma-separated single flag `--github-repos "owner1/repo1,owner2/repo2"`
- **Authentication**: `GITHUB_TOKEN` environment variable only; public repos work without token
- **Category strategy**: Last directory name only (e.g., `docs/v2/bank/account/overview.md` → Category: "account"), maintaining existing string type compatibility
- **Git implementation**: go-git library (pure Go, cross-platform, testable)
- **Clone storage**: OS temp directory, cleanup after processing
- **Tests**: Tests after implementation (not TDD)

**Research Findings**:
- S3Scanner provides an excellent pattern to follow: `ScanBucket()` → list files, `DownloadFile()` → get content
- FileInfo.SourceType already supports "local"/"s3" — extending to "github" is natural
- hashstore.ChangeDetector accepts sourceTypes slice — adding "github" is straightforward
- MetadataExtractor.extractCategory falls back to directory name from path — GitHub can leverage this with path manipulation
- Vectorize command already handles multi-source (local + S3) — adding GitHub follows the same pattern

### Self-Analysis (Metis equivalent)
**Identified Gaps** (addressed):
- **Branch selection**: Default branch only (main/master). Explicitly excluded branch switching from scope.
- **Large repos**: go-git clone depth should be limited to `--depth 1` (shallow clone) to avoid downloading full history.
- **Rate limiting**: GitHub API rate limits for authenticated/unauthenticated requests. go-git uses HTTPS so standard git rate limits apply.
- **Repo validation**: Invalid repo format in `--github-repos` flag needs validation (must be "owner/repo" format).
- **Temp dir cleanup**: Must handle cleanup even on error/panic (defer pattern).
- **File path for Reference**: Construct GitHub web URL from owner/repo/branch/filepath.
- **.gitignore handling**: go-git doesn't automatically respect .gitignore when walking the cloned repo filesystem — this is fine since we only care about .md/.csv files.

---

## Work Objectives

### Core Objective
Enable RAGent to clone GitHub repositories and vectorize their markdown/CSV files with automatically generated metadata, seamlessly integrated into the existing vectorize pipeline.

### Concrete Deliverables
- New file: `internal/scanner/github_scanner.go`
- New file: `internal/scanner/github_scanner_test.go`
- Modified: `cmd/vectorize.go` (new flags, GitHub source scanning)
- Modified: `internal/metadata/extractor.go` (GitHub metadata logic)
- Modified: `internal/types/types.go` (GitHub config fields)
- Modified: `README.md` and `README_ja.md` (documentation)

### Definition of Done
- [x] `RAGent vectorize --github-repos "simply-app/simply-docs"` clones and vectorizes .md/.csv files
- [x] `RAGent vectorize --github-repos "owner1/repo1,owner2/repo2"` handles multiple repos
- [x] `RAGent vectorize --follow --github-repos "..."` periodically re-fetches and vectorizes
- [x] Metadata auto-populated: Author=owner, Source=repo, Category=last dir, Reference=GitHub URL
- [x] `go test ./internal/scanner/...` passes with GitHub scanner tests
- [x] `go vet ./...` passes with zero warnings
- [x] Temp directories cleaned up after processing

### Must Have
- go-git based clone/fetch with shallow clone (depth 1)
- `GITHUB_TOKEN` environment variable support for private repos
- Comma-separated `--github-repos` flag for multiple repositories
- Automatic metadata: owner → Author, repo → Source, last dir → Category, GitHub URL → Reference
- SourceType "github" for hashstore change detection
- Follow mode compatibility
- Temp directory cleanup (even on error)

### Must NOT Have (Guardrails)
- NO branch selection (default branch only)
- NO GitHub API usage (use go-git HTTPS clone only)
- NO webhook/GitHub Actions integration
- NO PR/Issue content extraction
- NO persistent clone cache (temp dir only, per user's decision)
- NO modification to DocumentMetadata.Category type (keep string)
- NO modification to existing "local" or "s3" scanner behavior
- NO vendor directory (`go mod vendor` is prohibited per AGENTS.md)
- AI slop prevention: NO unnecessary abstractions or interfaces beyond what exists

---

## Verification Strategy

> **UNIVERSAL RULE: ZERO HUMAN INTERVENTION**
>
> ALL tasks are verifiable WITHOUT any human action.

### Test Decision
- **Infrastructure exists**: YES (go test, testify)
- **Automated tests**: YES (tests-after)
- **Framework**: go test + testify

### Agent-Executed QA Scenarios (MANDATORY — ALL tasks)

Verification is done via CLI commands and go test. Each task includes specific QA scenarios.

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start Immediately):
├── Task 1: Types & Config updates (no dependencies)
└── Task 2: GitHub Scanner implementation (no dependencies, uses types from Task 1 but types are simple enough to define inline first)

Actually, due to Go type dependencies:

Wave 1 (Sequential core):
├── Task 1: Types & Config → Task 2: Scanner → Task 3: Metadata → Task 4: Vectorize integration
Wave 2 (After Wave 1):
├── Task 5: Tests (depends on all implementation)
└── Task 6: Documentation (depends on final API)
```

### Dependency Matrix

| Task | Depends On | Blocks | Can Parallelize With |
|------|------------|--------|---------------------|
| 1 | None | 2, 3, 4 | None |
| 2 | 1 | 4 | None |
| 3 | 1 | 4 | 2 (partially) |
| 4 | 1, 2, 3 | 5, 6 | None |
| 5 | 4 | None | 6 |
| 6 | 4 | None | 5 |

### Agent Dispatch Summary

| Wave | Tasks | Recommended Agents |
|------|-------|-------------------|
| 1 | 1→2→3→4 | Sequential: task(category="deep", load_skills=[], ...) |
| 2 | 5, 6 | Parallel: task(category="unspecified-low", ...) |

---

## TODOs

- [x] 1. Add GitHub configuration fields to types

  **What to do**:
  - Add `GITHUB_TOKEN` field to `types.Config` struct with env tag
  - The `--github-repos` flag will be handled as a command-level variable in `cmd/vectorize.go` (same pattern as `--s3-bucket`)
  - No new types needed — reuse existing `FileInfo` with SourceType="github"

  **Must NOT do**:
  - Do NOT add new type structs unless absolutely necessary
  - Do NOT modify existing field types

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Small, localized change to a single file
  - **Skills**: []
    - No special skills needed for a type addition

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1, Sequential position 1
  - **Blocks**: Tasks 2, 3, 4
  - **Blocked By**: None

  **References**:

  **Pattern References**:
  - `internal/types/types.go:115-202` — Config struct with env tags pattern. Follow same convention for `GITHUB_TOKEN` field.

  **API/Type References**:
  - `internal/types/types.go:24-36` — FileInfo struct. SourceType field already exists, just needs "github" value.
  - `internal/types/types.go:9-21` — DocumentMetadata struct. Author, Source, Reference, Category fields will be populated by GitHub scanner.

  **Test References**:
  - No test modification needed for this task

  **Acceptance Criteria**:

  - [x] `types.Config` has `GitHubToken string` field with `env:"GITHUB_TOKEN"` tag
  - [x] `go build ./...` compiles without errors
  - [x] `go vet ./...` passes

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: Build succeeds after type changes
    Tool: Bash
    Preconditions: Go toolchain available
    Steps:
      1. go build ./...
      2. Assert: exit code 0
      3. go vet ./...
      4. Assert: exit code 0
    Expected Result: Clean build and vet
    Evidence: Command output captured
  ```

  **Commit**: YES (groups with Task 2)
  - Message: `feat(types): add GitHub token config field`
  - Files: `internal/types/types.go`
  - Pre-commit: `go build ./...`

---

- [x] 2. Implement GitHub Scanner

  **What to do**:
  - Create `internal/scanner/github_scanner.go`
  - Implement `GitHubScanner` struct with fields: `repos []GitHubRepo`, `token string`, `tempDir string`
  - Define `GitHubRepo` struct: `Owner string`, `Name string` (parsed from "owner/repo" string)
  - Implement `ParseGitHubRepos(reposStr string) ([]GitHubRepo, error)` — parse comma-separated "owner/repo" format
  - Implement `CloneOrFetch(ctx context.Context, repo GitHubRepo) (string, error)` — clone repo to temp dir using go-git with depth=1
  - Implement `ScanRepository(ctx context.Context, repo GitHubRepo, repoDir string) ([]*types.FileInfo, error)` — walk dir for .md/.csv files
  - Implement `ScanAllRepositories(ctx context.Context) ([]*types.FileInfo, error)` — iterate over all repos
  - Implement `Cleanup()` — remove temp directories
  - Set `FileInfo.SourceType = "github"` for all scanned files
  - Set `FileInfo.ContentHash` using existing `ComputeMD5Hash()` function
  - Construct metadata-friendly paths: store original repo-relative path in FileInfo.Path as `github://owner/repo/path/to/file.md`
  - Authentication: use `GITHUB_TOKEN` for HTTPS clone URL with token if provided
  - Handle errors gracefully: if one repo fails, log warning and continue with others

  **Must NOT do**:
  - Do NOT use GitHub API (REST or GraphQL) — use go-git HTTPS clone only
  - Do NOT cache cloned repos between runs (temp dir only)
  - Do NOT support branch selection (default branch only)
  - Do NOT create unnecessary abstractions/interfaces

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Core implementation requiring understanding of go-git library, file system operations, and existing scanner patterns
  - **Skills**: []
    - No browser or special skills needed

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1, Sequential position 2
  - **Blocks**: Task 4
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `internal/scanner/s3_scanner.go:20-54` — S3Scanner struct and constructor pattern. GitHubScanner should follow similar structure.
  - `internal/scanner/s3_scanner.go:57-137` — ScanBucket method. ScanAllRepositories should follow similar listing pattern.
  - `internal/scanner/s3_scanner.go:139-166` — DownloadFile method. File reading follows similar read-and-return pattern.
  - `internal/scanner/scanner.go:27-75` — ScanDirectory method with filepath.WalkDir. Reuse for walking cloned repo.
  - `internal/scanner/scanner.go:114-128` — IsSupportedFile, IsMarkdownFile, IsCSVFile. Reuse these helper methods.
  - `internal/scanner/scanner.go:142-158` — LoadFileWithContentAndHash. Follow same hash computation pattern.

  **API/Type References**:
  - `internal/types/types.go:24-36` — FileInfo struct to populate
  - `internal/scanner/scanner.go:155-158` — ComputeMD5Hash function to reuse

  **External References**:
  - go-git docs: `https://pkg.go.dev/github.com/go-git/go-git/v5` — Clone, PlainClone, PlainCloneContext APIs
  - go-git auth: `https://pkg.go.dev/github.com/go-git/go-git/v5/plumbing/transport/http` — BasicAuth with token

  **WHY Each Reference Matters**:
  - S3Scanner is the closest analog — same "scan external source, list files, read content" pattern
  - ComputeMD5Hash must be reused (not reimplemented) for hashstore compatibility
  - go-git BasicAuth with token as password is the standard pattern for GitHub HTTPS auth

  **Acceptance Criteria**:

  - [x] File `internal/scanner/github_scanner.go` exists
  - [x] `ParseGitHubRepos("owner1/repo1,owner2/repo2")` returns 2 GitHubRepo structs
  - [x] `ParseGitHubRepos("invalid")` returns error
  - [x] `go build ./internal/scanner/...` compiles
  - [x] `go vet ./internal/scanner/...` passes

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: Build scanner package successfully
    Tool: Bash
    Preconditions: go-git dependency added to go.mod
    Steps:
      1. go build ./internal/scanner/...
      2. Assert: exit code 0
      3. go vet ./internal/scanner/...
      4. Assert: exit code 0
    Expected Result: Scanner package compiles cleanly
    Evidence: Command output captured

  Scenario: Verify go-git dependency added
    Tool: Bash
    Steps:
      1. grep "go-git" go.mod
      2. Assert: output contains "github.com/go-git/go-git/v5"
    Expected Result: go-git dependency present in go.mod
    Evidence: grep output captured
  ```

  **Commit**: YES
  - Message: `feat(scanner): add GitHub repository scanner with go-git`
  - Files: `internal/scanner/github_scanner.go`, `go.mod`, `go.sum`
  - Pre-commit: `go build ./internal/scanner/...`

---

- [x] 3. Extend Metadata Extractor for GitHub sources

  **What to do**:
  - Add `ExtractGitHubMetadata(repoOwner, repoName, repoRelativePath, content string) (*DocumentMetadata, error)` method to MetadataExtractor
  - Logic:
    - Try parsing frontmatter first (existing `ParseFrontMatter`)
    - If no frontmatter title: extract from first `# heading` or filename
    - Category: `filepath.Base(filepath.Dir(repoRelativePath))` — last directory name. If root, use "general"
    - Author: `repoOwner`
    - Source: `repoName`
    - Reference: `fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", owner, repo, relativePath)`
    - Tags: `[]string{repoOwner, repoName}` as default (overridable by frontmatter)
  - This method should be called by the vectorize command when processing GitHub-sourced FileInfo entries

  **Must NOT do**:
  - Do NOT modify existing `ExtractMetadata` method behavior
  - Do NOT change DocumentMetadata.Category type from string
  - Do NOT hardcode branch names other than "main" in Reference URL (use detected default branch if possible, fallback to "main")

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Focused addition of one method to existing file, following established patterns
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1, Sequential position 3
  - **Blocks**: Task 4
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `internal/metadata/extractor.go:27-72` — ExtractMetadata method. GitHubMetadata method follows same structure but with GitHub-specific field population.
  - `internal/metadata/extractor.go:225-244` — extractCategory method. Shows existing category extraction from path pattern.
  - `internal/metadata/extractor.go:196-211` — extractTitle method. Reuse same title extraction logic (frontmatter → heading → filename).

  **API/Type References**:
  - `internal/types/types.go:9-21` — DocumentMetadata struct fields to populate

  **WHY Each Reference Matters**:
  - ExtractMetadata shows the full metadata population pattern to follow
  - extractCategory shows how directory-based category works — GitHub version just ensures last dir only
  - extractTitle shows the fallback chain (frontmatter → heading → filename) to reuse

  **Acceptance Criteria**:

  - [x] `ExtractGitHubMetadata` method exists on MetadataExtractor
  - [x] For path `docs/v2/bank/account/overview.md`: Category = "account", not "bank" or "docs"
  - [x] Author = owner name, Source = repo name
  - [x] Reference contains GitHub URL format
  - [x] Frontmatter fields override auto-generated values when present
  - [x] `go build ./internal/metadata/...` compiles
  - [x] `go vet ./internal/metadata/...` passes

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: Build metadata package successfully
    Tool: Bash
    Steps:
      1. go build ./internal/metadata/...
      2. Assert: exit code 0
      3. go vet ./internal/metadata/...
      4. Assert: exit code 0
    Expected Result: Metadata package compiles cleanly
    Evidence: Command output captured
  ```

  **Commit**: YES
  - Message: `feat(metadata): add GitHub-specific metadata extraction`
  - Files: `internal/metadata/extractor.go`
  - Pre-commit: `go build ./internal/metadata/...`

---

- [x] 4. Integrate GitHub source into vectorize command

  **What to do**:
  - Add command-level variables in `cmd/vectorize.go`:
    - `githubRepos string` — comma-separated "owner/repo" list
  - Add flags in `init()`:
    - `--github-repos` — comma-separated list of GitHub repositories to clone and vectorize
  - In `executeVectorizationOnceWithProgress`:
    - Add `hasGitHubSource := githubRepos != ""` check
    - Validate at least one source: local, S3, or GitHub
    - Parse repos with `scanner.ParseGitHubRepos(githubRepos)`
    - Create `scanner.NewGitHubScanner(repos, cfg.GitHubToken)` 
    - Call `githubScanner.ScanAllRepositories(ctx)` to get files
    - For each GitHub file: call `metadataExtractor.ExtractGitHubMetadata(...)` to set metadata on FileInfo
    - Append GitHub files to `allFiles`
    - Ensure `defer githubScanner.Cleanup()` for temp dir cleanup
    - Add "github" to sourceTypes for hashstore change detection
  - In `runFollowMode`: GitHub repos are re-cloned/fetched each cycle (no persistent cache)

  **Must NOT do**:
  - Do NOT refactor existing local/S3 source handling
  - Do NOT change the existing flag patterns
  - Do NOT add GitHub-specific IPC status messages (use existing pattern)

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Integration task touching multiple code paths, requires understanding of the full vectorize pipeline
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1, Sequential position 4
  - **Blocks**: Tasks 5, 6
  - **Blocked By**: Tasks 1, 2, 3

  **References**:

  **Pattern References**:
  - `cmd/vectorize.go:91-113` — init() flag definitions. Add `--github-repos` following same pattern.
  - `cmd/vectorize.go:170-180` — Source validation pattern (hasLocalSource, hasS3Source). Add hasGitHubSource.
  - `cmd/vectorize.go:302-357` — S3 source scanning block. GitHub source follows exact same pattern.
  - `cmd/vectorize.go:370-407` — hashstore change detection with sourceTypes. Add "github" to the slice.
  - `cmd/vectorize.go:35-63` — Command-level variables. Add `githubRepos string`.

  **API/Type References**:
  - `internal/scanner/github_scanner.go` — NewGitHubScanner, ScanAllRepositories, Cleanup (from Task 2)
  - `internal/metadata/extractor.go` — ExtractGitHubMetadata (from Task 3)
  - `internal/types/types.go:115` — Config.GitHubToken (from Task 1)

  **WHY Each Reference Matters**:
  - S3 source scanning block (lines 302-357) is the EXACT pattern to replicate for GitHub
  - Source validation (lines 170-180) shows where to add the third source check
  - hashstore sourceTypes (lines 385-392) must include "github" for change detection

  **Acceptance Criteria**:

  - [x] `--github-repos` flag registered and accepted by vectorize command
  - [x] `RAGent vectorize --github-repos "owner/repo"` clones repo and finds .md/.csv files
  - [x] `RAGent vectorize --github-repos "owner/repo" --dry-run` shows files without processing
  - [x] GitHub source works alongside local and S3 sources
  - [x] Temp directories cleaned up after processing (even on error)
  - [x] `go build -o RAGent` compiles
  - [x] `go vet ./...` passes

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: Build full binary with GitHub support
    Tool: Bash
    Steps:
      1. go build -o RAGent
      2. Assert: exit code 0
      3. ./RAGent vectorize --help
      4. Assert: output contains "--github-repos"
    Expected Result: Binary builds, help shows new flag
    Evidence: Help output captured

  Scenario: Validate flag parsing for invalid repo format
    Tool: Bash
    Steps:
      1. go vet ./...
      2. Assert: exit code 0
      3. go build -o RAGent
      4. Assert: exit code 0
    Expected Result: Clean build and vet
    Evidence: Command output captured

  Scenario: Dry-run with public GitHub repo
    Tool: Bash
    Preconditions: Network access available, OPENSEARCH_ENDPOINT and OPENSEARCH_INDEX set (or mocked)
    Steps:
      1. ./RAGent vectorize --github-repos "simply-app/simply-docs" --dry-run
      2. Assert: output lists .md files found
      3. Assert: output does NOT show API call errors
      4. Assert: exit code 0
    Expected Result: Dry run shows files from GitHub repo
    Evidence: Command output captured
  ```

  **Commit**: YES
  - Message: `feat(vectorize): integrate GitHub repository source with --github-repos flag`
  - Files: `cmd/vectorize.go`
  - Pre-commit: `go build -o RAGent`

---

- [x] 5. Add unit tests for GitHub Scanner and Metadata

  **What to do**:
  - Create `internal/scanner/github_scanner_test.go`
  - Test `ParseGitHubRepos`:
    - Valid single repo: `"owner/repo"` → 1 GitHubRepo
    - Valid multiple repos: `"owner1/repo1,owner2/repo2"` → 2 GitHubRepo
    - Invalid format: `"invalid"` → error
    - Empty string: `""` → error
    - Whitespace handling: `" owner/repo , owner2/repo2 "` → 2 repos trimmed
  - Test `NewGitHubScanner`:
    - Creates scanner with parsed repos
    - Handles empty token (public repos)
  - Test metadata extraction for GitHub:
    - `docs/v2/bank/account/overview.md` → Category: "account"
    - `README.md` (root level) → Category: "general"
    - File with frontmatter → frontmatter values override auto-generated
    - Author = owner, Source = repo, Reference contains GitHub URL
  - Use testify for assertions (existing project pattern)

  **Must NOT do**:
  - Do NOT test actual git clone (network-dependent) — mock or use test fixtures
  - Do NOT modify existing test files

  **Recommended Agent Profile**:
  - **Category**: `unspecified-low`
    - Reason: Standard test writing following existing patterns
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Task 6)
  - **Blocks**: None
  - **Blocked By**: Task 4

  **References**:

  **Pattern References**:
  - `internal/scanner/s3_scanner_test.go` — S3Scanner test patterns, mocking approach
  - `internal/hashstore/detector_test.go` — Test structure with testify assertions
  - `tests/unit/extractor_test.go` — Metadata extractor test patterns

  **Test References**:
  - `internal/slacksearch/searcher_test.go` — testify assert/require patterns used across the project

  **WHY Each Reference Matters**:
  - S3Scanner test shows how to test a scanner without actual external service
  - extractor_test.go shows metadata assertion patterns to follow

  **Acceptance Criteria**:

  - [x] `internal/scanner/github_scanner_test.go` exists
  - [x] `go test -v ./internal/scanner/...` passes all GitHub scanner tests
  - [x] `go test -v -run TestExtractGitHubMetadata ./tests/unit/...` or `./internal/metadata/...` passes
  - [x] Test coverage includes: repo parsing, category extraction, metadata population, error cases

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: All GitHub scanner tests pass
    Tool: Bash
    Steps:
      1. go test -v ./internal/scanner/... -run GitHub
      2. Assert: exit code 0
      3. Assert: output contains "PASS"
      4. Assert: output does NOT contain "FAIL"
    Expected Result: All GitHub-related tests pass
    Evidence: Test output captured

  Scenario: All metadata tests pass (including new GitHub tests)
    Tool: Bash
    Steps:
      1. go test -v ./internal/metadata/...
      2. Assert: exit code 0
      3. Assert: output contains "PASS"
    Expected Result: Metadata tests pass including GitHub additions
    Evidence: Test output captured
  ```

  **Commit**: YES
  - Message: `test(scanner): add unit tests for GitHub scanner and metadata extraction`
  - Files: `internal/scanner/github_scanner_test.go`, possibly `internal/metadata/extractor_test.go` or `tests/unit/extractor_test.go`
  - Pre-commit: `go test ./internal/scanner/... ./internal/metadata/...`

---

- [x] 6. Update documentation

  **What to do**:
  - Update `README.md`:
    - Add GitHub to data sources in Features section
    - Add `--github-repos` flag to vectorize command options
    - Add GitHub example to vectorize command examples
    - Add `GITHUB_TOKEN` to Required Environment Variables section (as optional)
    - Update Architecture diagram to include GitHub as a data source
    - Update Project Structure section if new files created
  - Update `README_ja.md` with equivalent changes (Japanese translation)
  - Update `doc/` files if relevant

  **Must NOT do**:
  - Do NOT change documentation for unrelated features
  - Do NOT add implementation details to user-facing docs

  **Recommended Agent Profile**:
  - **Category**: `writing`
    - Reason: Documentation writing task
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Task 5)
  - **Blocks**: None
  - **Blocked By**: Task 4

  **References**:

  **Documentation References**:
  - `README.md` — Current documentation structure. Search for "S3 Source Examples" section to see pattern for adding GitHub examples.
  - `README_ja.md` — Japanese version that must be kept in sync per AGENTS.md rules.

  **WHY Each Reference Matters**:
  - README.md S3 examples section shows exactly how to document a new data source
  - AGENTS.md explicitly requires both README files to be updated together

  **Acceptance Criteria**:

  - [x] README.md mentions `--github-repos` flag
  - [x] README.md has GitHub usage examples
  - [x] README.md has `GITHUB_TOKEN` in environment variables
  - [x] README_ja.md has equivalent Japanese content
  - [x] No broken markdown formatting

  **Agent-Executed QA Scenarios:**

  ```
  Scenario: README contains GitHub documentation
    Tool: Bash (grep)
    Steps:
      1. grep -c "github-repos" README.md
      2. Assert: count >= 2 (flag definition + example)
      3. grep -c "GITHUB_TOKEN" README.md
      4. Assert: count >= 1
      5. grep -c "github-repos" README_ja.md
      6. Assert: count >= 2
    Expected Result: Both READMEs document GitHub features
    Evidence: grep output captured
  ```

  **Commit**: YES
  - Message: `docs: add GitHub data source documentation`
  - Files: `README.md`, `README_ja.md`
  - Pre-commit: none

---

## Commit Strategy

| After Task | Message | Files | Verification |
|------------|---------|-------|--------------|
| 1+2 | `feat(scanner): add GitHub repository scanner with go-git` | `internal/types/types.go`, `internal/scanner/github_scanner.go`, `go.mod`, `go.sum` | `go build ./...` |
| 3 | `feat(metadata): add GitHub-specific metadata extraction` | `internal/metadata/extractor.go` | `go build ./...` |
| 4 | `feat(vectorize): integrate GitHub repository source` | `cmd/vectorize.go` | `go build -o RAGent` |
| 5 | `test(scanner): add GitHub scanner unit tests` | `internal/scanner/github_scanner_test.go` | `go test ./internal/scanner/...` |
| 6 | `docs: add GitHub data source documentation` | `README.md`, `README_ja.md` | N/A |

---

## Success Criteria

### Verification Commands
```bash
# Build
go build -o RAGent                              # Expected: exit code 0

# Lint
go vet ./...                                     # Expected: exit code 0
go fmt ./...                                     # Expected: no changes

# Tests
go test ./internal/scanner/...                   # Expected: PASS
go test ./internal/metadata/...                  # Expected: PASS
go test ./...                                    # Expected: PASS (all existing tests still pass)

# Integration check (requires network)
./RAGent vectorize --github-repos "simply-app/simply-docs" --dry-run  # Expected: lists .md files
```

### Final Checklist
- [x] All "Must Have" present
- [x] All "Must NOT Have" absent
- [x] All tests pass (`go test ./...`)
- [x] `go vet ./...` clean
- [x] Temp directories cleaned up
- [x] Both README files updated
- [x] No existing functionality broken
