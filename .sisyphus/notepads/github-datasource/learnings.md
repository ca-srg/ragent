# Learnings

## 2026-02-10 Task: Initial Analysis
- Config struct is in `internal/types/types.go` with `env:` tags for environment variable binding
- FileInfo.SourceType currently supports "local" and "s3"
- S3Scanner in `internal/scanner/s3_scanner.go` is the pattern to follow for GitHubScanner
- MetadataExtractor in `internal/metadata/extractor.go` has `ExtractMetadata` method — add `ExtractGitHubMetadata`
- `cmd/vectorize.go` has command-level variables (line 35-63), flag registration (line 91-113), source validation (line 170-180), S3 scanning block (line 302-357), hashstore sourceTypes (line 385-392)
- ComputeMD5Hash is a package-level function in scanner.go (line 154-158)
- Import groups: stdlib, third-party, internal
- All files use `github.com/ca-srg/ragent/internal/types` for shared types

## 2026-02-10 Task: Implementation Complete
- go-git v5.16.5 added as dependency; requires `go get github.com/go-git/go-git/v5` AND `go get github.com/go-git/go-git/v5/plumbing/transport/http`
- ParseGitHubRepos uses SplitN(part, "/", 3) — rejects paths with more than one slash (e.g., "owner/repo/extra")
- GitHubScanner.CloneRepository uses depth=1 shallow clone for efficiency
- .git directory must be explicitly skipped in filepath.WalkDir
- github:// path scheme: `github://owner/repo/relative/path.md`
- parseGitHubPath helper in cmd/vectorize.go extracts owner/repo/relativePath from github:// paths
- hashstore sourceTypes changed from if/else chain to append-based construction for extensibility
- Pre-existing test failure: TestValidateFollowModeFlags/interval_too_short — not related to our changes
- 5 atomic commits following conventional commit style
