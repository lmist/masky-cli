# Security

Use GitHub's private vulnerability reporting for sensitive reports. For ordinary
bugs, open an issue with synthetic examples and redacted diagnostics.

- Stored profile tokens and saved responses use 0600 permissions on Unix, with
  atomic file replacement. Config symlinks and group/world-readable config files
  are rejected. Windows uses the current user's inherited ACLs; protect the
  configuration directory with Windows permissions.
- Environment credentials override stored credentials. Stored tokens are bound
  to their saved base URL. Passing an environment token with a custom base URL
  explicitly sends that token to that URL.
- HTTPS is required except localhost. `--allow-insecure` deliberately permits
  remote HTTP; it does not disable TLS certificate verification.
- API requests do not follow redirects. Media downloads use a separate client
  without API authorization, validate redirect schemes, and limit size to 1 GiB.
- API JSON responses are limited to 32 MiB and input files/stdin to 1 MiB.
- Diagnostic responses and dry runs redact known credentials. Successful API
  output can contain tokens, client secrets, and private viewer links by design.
  Do not publish raw successful responses without inspecting them.
- OAuth authorization creates cryptographic state and an S256 PKCE verifier in
  a private session file. Exchange verifies state, callback location, API base,
  and session age. A successful exchange consumes that file.
- API-key creation, gift confirmation, and agent binding tests use explicit
  first-party Firebase session tokens. Nostr callbacks require a pre-signed
  event from the bound agent key. The CLI never bypasses Masky's authorization.
- Writes are sent once. Deletion, revocation, paid tribe joins, gift confirmation,
  and brain sandbox deletion require `--yes`. The direct `api` escape hatch
  intentionally exposes lower-level calls; its DELETE calls also require `--yes`.

Local filesystem privacy assumes the user's account and directory permissions
are trusted. This CLI does not implement a hardware key store, filesystem
encryption, or a browser-based Firebase login. Account authentication and
authorization remain Masky's responsibility.
