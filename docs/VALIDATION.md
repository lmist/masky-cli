# Validation report

Validated against the public Masky API and its documentation on 2026-10-12.

## Automated checks

- Independent HTTP fixtures cover all **59 named API operations**: expected
  method, path, query encoding, body, authentication mode, and one-request
  behavior are asserted against isolated local servers.
- Validation tests cover required values, enum/type/range limits, conflicting
  inputs, null/false/omission, JSON/file/stdin input, authentication restrictions,
  unsafe paths, and refusing invalid input before any network request.
- Workflow tests cover video polling and downloads, nested speech responses,
  waiting for video after audio is ready, selecting the correct conversation
  turn, failures, deadlines, cancellation, and donation terminal states.
- Security tests cover GET-only retries, redirect refusal, bounded response
  reads, integer precision, private/atomic output, credential-origin binding,
  config permissions and symlinks, redaction, unauthenticated media downloads,
  PKCE (including the RFC test vector), callback state, and session expiry.
- Completion scripts are generated for bash, zsh, fish, and PowerShell.
- Final local statement coverage is **75.0% overall**, **80.2%** in the HTTP
  package, and **79.5%** in the CLI package. The overall figure includes the
  executable and documentation generator entry points.
- `go test -race ./...`, `go vet ./...`, formatting, and generated docs are
  required by CI, alongside six-platform packaging.
- `govulncheck` reports **no vulnerabilities** with Go **1.27.2**. The first scan
  exposed standard-library advisories in the host's Go 1.27.1; release and CI
  builds were moved to an isolated patched toolchain.
- Release archives cross-compile for darwin/linux/windows × amd64/arm64 and
  include shell completions, README, license, attribution, and SHA-256 checksums.

## Live smoke test

Real requests used a user-supplied key held outside the repository. Neither the
key nor raw account responses are committed.

| Workflow | Result |
| --- | --- |
| Own avatars, voices, pricing, folders, OAuth clients | All read successfully |
| Generate image and download | Completed; 71,142-byte local file |
| Owned avatar speech, wait, download | Completed; 10,700-byte local audio file |
| One-second draft video, wait, download | Completed; 60,515-byte local MP4 |
| Create private conversation | Completed |
| Add text turn and poll exact turn | Completed; text result ready |

Reported costs: image **0.005**, speech **0.004**, video **0.0156**, text turn
**0** credits: **0.0246 credits total**. These are observed smoke-test charges,
not a pricing guarantee. Masky's documentation equates one credit to one dollar.

The test created media and one private conversation on the testing account.
It did not modify existing avatars, render someone else's avatar, transfer
credits, join a tribe, create/revoke keys, or change public visibility.

## Coverage limits

The live test is representative. Firebase-only routes, OAuth consent, Nostr
signing, brain memory, translations, financial actions, and destructive routes
are tested with synthetic contracts; they were not executed against the account.
Prose-only contracts may drift independently of OpenAPI. No unverified live
success is implied by endpoint coverage.
