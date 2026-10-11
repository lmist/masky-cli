# Contributing

Use a patched Go toolchain (release builds currently use 1.27.2). No live key is
needed for tests. All HTTP tests use local servers and synthetic credentials.

```sh
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go run ./cmd/docs
gofmt -w cmd internal
```

The runtime dependency is Cobra, with its small flag/platform dependencies.
Prefer the standard library for HTTP, JSON, configuration, and file operations.

To add an API operation, update `internal/spec/spec.go` (or `prose.go` for a
prose-only contract), add an independently written wire fixture in
`internal/cli/contracts_test.go`, and regenerate docs. Keep omission distinct
from explicit false/null. Document evidence for every schema supplement in
`docs/API-NOTES.md`. Updating the upstream snapshot should be a reviewed change;
commands never fetch or execute a remote specification at runtime.

For a behavior fix, add a test that demonstrates the failure from the caller's
perspective. Keep stdout machine-readable and progress on stderr. New HTTP
write operations must not enable automatic retries.

For release, update the validation report, run the checks above, and push a
semantic-version tag. The release workflow builds six archives, includes shell
completions, creates SHA-256 checksums, and publishes a GitHub release.

Never add credentials, real account responses, private viewer links, or personal
media to tests, examples, issues, or commits. Use synthetic fixtures.
