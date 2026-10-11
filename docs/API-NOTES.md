# API sources and reconciliation

Sources retrieved on 2026-10-12:

- [Full OpenAPI 1.2.0](https://masky.ai/api/docs/masky-openapi.json): 45 operations;
  unmodified snapshot in `internal/spec/openapi.json`.
- [Human documentation](https://masky.ai/api/docs): behavior, limits, examples,
  text-only/two-avatar turns, and OAuth scopes.
- [Linked API reference](https://masky.ai/masky-api-skill.md): 14 additional routes
  and details on chunking, translations, brain memory, and avatar folders.
- [Speak OpenAPI](https://masky.ai/api/docs/openapi.json): overlaps with the full
  spec and describes the short share redirect.

The CLI takes API facts from these sources; it does not execute instructions
from remotely supplied documents. Context7 did not index Masky at the time of
implementation; Cobra's official documentation was available through Context7.

## Explicit supplements

| Area | Difference and CLI behavior |
| --- | --- |
| Conversation output | Schema lists audio/video; prose also documents text. Accept all three. |
| Two-avatar turns | Add speakerAvatarId, userOutput, and avatarText from prose. Enforce avatarText's documented 500-character truncation limit before sending. |
| Avatar creation | Add folderId from the linked reference. |
| OAuth scopes | Schema omits tribes/donations on client registration. Accept all five documented scopes. |
| Image generation | Add the prose's 2,000-character prompt limit. |
| Image editing | Require exactly one source form, 1–5 references, at most 2 for Qwen, and documented aspect ratios. |
| Gifts | Apply the documented 0.01–1,000 credit range. |
| Nullable values | Accept both OpenAPI nullable and the mixed string/null type used by serviceAvatarId. Use `--set FIELD=null`. |
| Missing path metadata | Derive positional names from the path, including PATCH /oauth/clients/{clientId}. |
| Missing security metadata | Stream identity and client service-avatar updates still use bearer credentials; agent test uses an explicit Firebase session. |
| Share URLs | The speak spec's server prefix is ambiguous. Use the prose's canonical site-root /g-… and /t-… links, with no API bearer. |
| API responses | Preserve full JSON, tolerate additive fields and nested generation objects, and keep large integers exact. |

The 14 prose-only routes are avatar rename, three folder operations, three brain
memory operations, sandbox deletion, conversation access-link creation, bulk
translation, and four per-turn translation operations. Their contracts live in
`internal/spec/prose.go`, separately from the upstream snapshot.

## Deliberate boundaries

- Voice cloning, assigning a brain or agent binding, and some account settings
  are documented as web-UI operations. The CLI does not invent undocumented
  HTTP mutations for them. Agent binding test accepts the documented optional
  binding object; agent completion accepts a pre-signed Nostr header.
- The agent callback's `--dry-run` includes `bodySha256`, the SHA-256 of the exact
  compact JSON bytes the CLI sends. Sign that hash and the exact method/URL in
  the kind-27235 event, then pass `--authorization-file`. The CLI does not store
  Nostr private keys or implement a relay listener.
- Waiting for a turn tracks the first returned turn ID. The API does not expose
  a reliable final chunk count or parent-job completion marker in the published
  contract. Inferring that a conversation is complete from a quiet interval
  could silently truncate it, so additional chunks remain available through
  `conversations get` and the live player.
- Bulk translation uses idempotent POST to report progress, including HTTP 409
  while running. The CLI exposes this response but does not poll via repeated
  writes. Per-turn translation calls are synchronous and accept `--regenerate`.
- Access-link creation has no published request-field schema in the linked
  reference. The named command sends `{}` and allows explicit fields via
  `--data`; the CLI does not guess rotation or revocation flags.
- Upstream billing prose is inconsistent about rendering someone else's public
  avatar. The CLI does not assert who will pay. Check current pricing and use
  avatars you own for predictable smoke testing.
- A small live smoke test validates representative workflows, not all 59
  operations on a real account. Session-only, financial, destructive, and
  account-specific operations are covered by mock contracts.

New parameters can be passed through `--data` or `masky api` while waiting for a
reviewed contract update. Existing named fields continue to receive validation.
