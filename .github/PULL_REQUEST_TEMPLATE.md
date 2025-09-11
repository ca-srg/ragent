# Pull Request

## Summary
<!-- Provide a brief description of what this PR accomplishes -->

## Type of Change
<!-- Mark the relevant option with an "x" -->
- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update
- [ ] Performance improvement
- [ ] Refactoring (no functional changes)

## Changes Made
<!-- Describe the specific changes implemented in this PR -->
- 
- 
- 

## Motivation and Context
<!-- Why is this change required? What problem does it solve? -->
<!-- If it fixes an open issue, please link to the issue here -->
Fixes #(issue)

## How Has This Been Tested?
<!-- Describe the tests you ran to verify your changes -->
<!-- Provide instructions so reviewers can reproduce -->
- [ ] Unit tests (`go test ./...`)
- [ ] Integration tests
- [ ] Manual testing with local setup
- [ ] Tested with AWS services (S3 Vectors, OpenSearch, Bedrock)

### Test Configuration
- Go version:
- AWS Region:
- OpenSearch version (if applicable):

## Impact Analysis
<!-- What parts of the system does this change affect? -->
### Components Affected
- [ ] CLI commands (`cmd/`)
- [ ] Vectorization (`internal/vectorizer/`)
- [ ] OpenSearch integration (`internal/opensearch/`)
- [ ] S3 Vector operations (`internal/s3vector/`)
- [ ] Slack bot (`internal/slackbot/`)
- [ ] Bedrock embedding (`internal/embedding/`)
- [ ] Configuration (`internal/config/`)

### AWS Resources Impact
- [ ] No AWS resource changes
- [ ] S3 bucket operations
- [ ] OpenSearch index structure
- [ ] IAM permissions required
- [ ] Bedrock model usage

## Breaking Changes
<!-- List any breaking changes and migration steps if applicable -->
- [ ] None
- [ ] Yes (describe below)

### Migration Guide
<!-- If breaking changes, provide migration steps -->

## Dependencies
<!-- List any new dependencies added or updated -->
- [ ] No new dependencies
- [ ] Dependencies added/updated (list below)

## Documentation
<!-- Documentation changes required for this PR -->
- [ ] README.md updated
- [ ] CLAUDE.md updated
- [ ] Inline code comments added/updated
- [ ] API documentation updated
- [ ] Configuration examples updated

## Checklist
<!-- Mark completed items with an "x" -->
- [ ] My code follows the project's style guidelines (`go fmt ./...` and `go vet ./...`)
- [ ] I have performed a self-review of my own code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [ ] My changes generate no new warnings or errors
- [ ] I have added tests that prove my fix is effective or that my feature works
- [ ] New and existing unit tests pass locally with my changes
- [ ] Any dependent changes have been merged and published
- [ ] I have checked my code for any security issues or exposed secrets
- [ ] I have tested with the minimum supported Go version (1.23)
- [ ] I have run `go mod tidy` to clean up dependencies

## Performance Considerations
<!-- If applicable, describe any performance implications -->
- [ ] No performance impact
- [ ] Performance improved (describe metrics)
- [ ] Performance degraded but acceptable (explain trade-offs)

## Additional Notes
<!-- Any additional information that reviewers should know -->

## Screenshots/Logs
<!-- If applicable, add screenshots or logs to help explain your changes -->