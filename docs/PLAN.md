# Implementation plan and acceptance criteria

## Scope and stack

Deliver a public `lmist/masky-cli` repository with executable `masky`, covering
all 45 operations in the full Masky OpenAPI 1.2.0 snapshot fetched 2026-10-12,
14 additional operations in the linked API reference, prose-only conversation
fields, OAuth consent/PKCE, share links, and live
embedding. Full coverage means command and contract-test coverage; it does not
mean executing destructive or expensive operations on a live account.

Go + Cobra provides compiled cross-platform binaries, context-aware HTTP using
the standard library, built-in testing/race detection, help, and completions.
Python/Typer and TypeScript/Commander would require a runtime for end users;
Rust/clap adds build complexity without improving this HTTP-focused tool.

## Work breakdown

1. **Discovery and contract reconciliation.** Inventory both OpenAPI specs and
   prose docs. Commit the full spec snapshot with attribution. Map each method
   and path to exactly one named command. Document omissions and conflicts.
2. **Core infrastructure.** Add context-bound HTTP, safe read retries, bounded
   response sizes, structured errors and exit codes, redacted diagnostics,
   profile credentials with private permissions, explicit base URL selection,
   JSON output, scalar selection, tables, and input from files/stdin.
3. **API commands.** Implement images, videos, avatars/stills/voices/training,
   stream identity, conversations/turns/visibility, API keys, OAuth clients and
   grants, tribe membership, donation intents, and local-agent bridge calls.
   Validate required fields, enums, types, limits, and cross-field rules before
   sending. Preserve omitted vs explicit false/null. Support JSON bodies for
   forward compatibility.
4. **Workflows.** Implement deadline-bound video, speak, donation, and turn
   waiting; atomic media downloads without credential forwarding; OAuth PKCE
   consent URLs and code exchange; safe live-viewer URL and iframe generation;
   generic API escape hatch; bash/zsh/fish/PowerShell completions.
5. **Verification.** Table-driven HTTP contract tests for every endpoint,
   validation/no-request tests, polling lifecycle and cancellation tests,
   retries/redirect/error/redaction tests, config permissions/precedence tests,
   OAuth and download tests. Run format, vet, tests/race, vulnerability scan,
   coverage, cross-compilation, and actual binary smoke tests. Use a limited
   live test with the supplied key; never store it in the repository or logs.
6. **Delivery.** Write quickstart, generated command reference, coverage matrix,
   API drift notes, examples, contribution/security policy, and validation
   report. Add CI and tagged release packaging with SHA-256 checksums. Scan
   the complete publication tree for secrets, commit, publish, verify CI,
   then publish a versioned release with binaries.

## Architecture

- `internal/spec`: embedded upstream contract, explicit command map and
  documented supplements; the contract is pinned, never fetched at runtime.
- `internal/api`: HTTP, errors, read retries, and media downloads.
- `internal/config`: profile storage and credential lookup.
- `internal/secureio`: bounded input and private atomic output.
- `internal/cli`: command assembly, schema validation, output, auth and
  higher-level waiting/OAuth/share workflows.
- `cmd/masky`: signal-aware entry point and version metadata.
- `cmd/docs`: reproducible command reference and endpoint coverage generator.

## Risk decisions

- Never retry writes automatically: a timeout can occur after a billed request
  succeeds. Return a clear error and let the user inspect existing jobs.
- Authentication differs by route: ordinary bearer tokens, Firebase-only
  session routes, unauthenticated endpoints, and Nostr-signed agent callbacks
  must not be conflated.
- New API keys, OAuth tokens and viewer links are intentional output secrets;
  keep stdout usable, redact diagnostics/dry runs, and create output files 0600.
- Gift confirmation requires a Firebase session; the normal flow is the Masky
  confirmation page. The CLI must never suggest bypassing this requirement.
- Turn polling follows the requested media type and selected turn ID; it must
  not treat audio readiness as video completion or select an unrelated turn.
- Full endpoint testing uses an isolated mock server. Live smoke tests exclude
  gifts, joining paid tribes, revocation, visibility changes and other users'
  avatar generation.

## Done when

Every documented operation has a named, documented, contract-tested command;
checks pass; credential handling is reviewed; a small live workflow works or
its server-side limitation is recorded accurately; CI passes in the public
repository; installable binaries and source are available.
