# Masky CLI

[![CI](https://github.com/lmist/masky-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/lmist/masky-cli/actions/workflows/ci.yml)

Generate images and videos, make avatars speak, run conversations, manage brain
memories, and integrate Masky from your terminal. One binary. JSON by default.

An independent community CLI for the [Masky API](https://masky.ai/api/docs), built
with Go and Cobra. Covers **59 documented API operations**, including 14 routes
in Masky's linked reference that are missing from its OpenAPI schema.

## Install

Download the archive for your platform from [Releases](https://github.com/lmist/masky-cli/releases/latest),
verify it against `checksums.txt`, extract it, and put `masky` (`masky.exe` on
Windows) on your PATH. Binaries are available for macOS, Linux, and Windows,
on both x86-64 (`amd64`) and ARM64. No language runtime is needed.

Or install from source with a current patched Go toolchain:

```sh
go install github.com/lmist/masky-cli/cmd/masky@latest
masky --help
```

Release builds use Go 1.27.2. Source supports Go 1.24+, but use a maintained,
patched version for network-facing software. If `masky` is not found after
`go install`, add `$(go env GOPATH)/bin` to PATH.

## First request

Create a key at [masky.ai/developer](https://masky.ai/developer). Pass it through
`MASKY_API_KEY` in your secret manager or CI, or save it to a profile from stdin:

```sh
# key.txt contains only your token; keep it private.
masky auth login --token-file /path/to/private/key.txt
masky auth status
masky avatars list
masky voices list --format table
```

`auth login --token-stdin` reads a token piped from your secret manager. It does
not echo the token. Profiles are stored as plaintext in a file with mode 0600
on Unix; use the environment option if you do not want persistent storage.

## Generate and download

```sh
masky images generate --prompt 'A friendly robot barista' \
  --aspect-ratio 1:1 --download portrait.jpg

masky images edit --prompt 'Add a blue scarf' \
  --image-url https://example.com/portrait.jpg --download edited.jpg

masky videos generate --prompt 'Clouds drifting over a mountain lake' \
  --duration 5 --draft --wait --download lake.mp4

masky avatars speak AVATAR_ID --text 'Hello from the terminal.' \
  --output audio --wait --download hello.mp3

# Resume an existing job without creating or charging for another.
masky videos wait GENERATION_ID --download lake.mp4
masky speak wait GENERATION_ID --output video --download greeting.mp4
masky videos pricing
```

`--download` implies waiting for supported asynchronous jobs. Existing files
are preserved unless you pass `--force`. Progress goes to stderr and the final
API response goes to stdout. Use `--wait-timeout 15m` for longer renders and
`--timeout 5m` for long synchronous operations.

## Avatars and conversations

```sh
masky avatars create --display-name 'Barista Bot' \
  --image-url https://example.com/portrait.jpg \
  --personality-prompt 'A cheerful coffee expert.' --voice-id VOICE_ID

masky avatars images list AVATAR_ID
masky avatars rename AVATAR_ID --display-name 'Coffee Guide'
masky folders create --name 'Characters'

masky conversations create --avatar-id AVATAR_ID \
  --avatar-owner-user-id OWNER_ID --save-json conversation.json

masky conversations turn c-26-10-ab12 --user-text 'Explain espresso.' \
  --output text --wait

masky conversations turn c-26-10-ab12 --user-text 'Welcome to our show.' \
  --mode speak --output video --wait --download welcome.mp4

masky conversations get c-26-10-ab12
```

The `c-…` values above are placeholders; use the `shareSlug` returned by create.
When passing an internal conversation ID to `turn --wait`, also pass `--slug`.
Long replies can split into multiple turns: `--wait` and `--download` follow
the **first returned turn**. Read the full conversation or use its `liveUrl`
to play every chunk. Keep scripted clips within three sentences for one clip
per call. Private viewer URLs contain access tokens; handle them as secrets.

Two-avatar turns expose `--speaker-avatar-id`, `--user-output`, and
`--avatar-text`. Translation commands support captions, dubbed audio, and
lip-synced video:

```sh
masky conversations translate CONVERSATION_ID --cc es --audio es
masky conversations turns translation-video CONVERSATION_ID TURN_ID \
  --lang es --timeout 5m --download spanish.mp4
```

Bulk translation returns an asynchronous job. Re-run the same request to inspect
progress; Masky documents it as idempotent. HTTP 409 means a job is still running
and its progress is included in the error. The CLI does not automatically repeat
POST requests. Use `--regenerate` on per-turn translation commands to regenerate;
`--force` is reserved for replacing local output files.

## Command families

| Command | Capabilities |
| --- | --- |
| `images` | Generate, edit, download; default/Ideogram/Qwen models |
| `videos` | Generate, get, wait, pricing |
| `avatars` | Own/public lists, lookup, create, rename, train, voices, image variants, renderability, stream identity, speech, brain memory, agent test |
| `speak` | Get/wait for one-shot speech or talking-head jobs |
| `voices`, `folders` | Voice catalog and avatar organization |
| `conversations` | Create, turn, get, wait, visibility, avatar image, access links, translations, agent completion |
| `oauth` | Client/grant management, PKCE consent and exchange, service-avatar tokens, userinfo |
| `keys` | List, create with Firebase session, revoke |
| `tribes` | Info, membership, join |
| `donations` | Propose, get, wait, cancel, sent totals, Firebase-only confirm |
| `share` | Share URLs, live URLs/iframes, public redirect resolution |
| `auth` | Login, logout, status, profiles, use |
| `api` | Direct calls for new or advanced API fields |

Every command has `--help`. See the [complete command reference](docs/COMMANDS.md),
[API coverage matrix](docs/API-COVERAGE.md), and [examples](docs/EXAMPLES.md).

## Scripts and JSON input

```sh
masky avatars list --select avatars.0.avatarId --raw
masky voices list --save-json voices.json

masky videos generate --data @video.json --wait
cat video.json | masky videos generate --data - --wait
masky images generate --prompt-file prompt.txt

# Explicit false and null are preserved; omitted options use server defaults.
masky videos generate --prompt 'A simple scene' --save-audio=false --set image=null

# Inspect method, URL, redacted body, and exact request-body SHA-256.
masky avatars speak AVATAR_ID --text 'Hello.' --dry-run
masky api GET /avatars
```

Input precedence: JSON `--data`, then explicit flags/file inputs, then
`--set field=JSON`. Arrays use repeatable flags, e.g. `--images URL1 --images URL2`.
Unknown fields in `--data` are preserved for forward compatibility; documented
fields are validated. `--select` supports dot paths and numeric array indices;
it is not a jq interpreter. JSON numbers preserve their original precision.

Success payloads may intentionally include newly minted credentials. Diagnostics
and dry runs redact credentials; protect your stdout and saved response files.
`--error-format json` emits `{ "error": "…", "exitCode": N }` on stderr.

| Exit code | Meaning |
| --- | --- |
| 0 | Success (for donation wait: any terminal state; inspect `status`) |
| 1 | Network, file, server, or other error |
| 2 | Invalid input or usage |
| 3 | Missing credentials / HTTP 401 or 403 |
| 4 | Insufficient credits / HTTP 402 |
| 5 | Rate limited after retries / HTTP 429 |
| 6 | Request or polling timeout |
| 7 | Generation failure or missing required result media |
| 130 | Interrupted |

Only GET requests retry transient failures (429/502/503/504 and transport errors).
POST/PUT/PATCH/DELETE never retry automatically. A timed-out write may already
have succeeded; inspect the job before creating another. No API bearer is sent
to media download servers or HTTP redirect targets.

## Configuration and completion

Credential precedence: `MASKY_API_KEY`, then the selected profile. Profile
selection: `--profile`, `MASKY_PROFILE`, then the stored default. Base URL:
`--base-url`, `MASKY_BASE_URL`, then the profile/default `https://masky.ai/api`.
Stored tokens are used only with the exact base URL they were saved for.

Config path: `--config`, `MASKY_CONFIG`, `$XDG_CONFIG_HOME/masky/config.json`,
then the OS configuration directory (`~/Library/Application Support/masky` on
macOS, `~/.config/masky` on Linux, `%AppData%\masky` on Windows).

```sh
masky auth login --profile secondary --token-stdin
masky auth use secondary
masky auth profiles

masky completion bash > masky.bash
masky completion zsh > _masky
masky completion fish > masky.fish
masky completion powershell > masky.ps1
```

`MASKY_SESSION_TOKEN` is reserved for Firebase-only routes. Ordinary API keys
cannot create another key, confirm gifts, or authenticate the agent-test route.
Nostr agent callbacks accept a signed authorization file. See
[authentication examples](docs/EXAMPLES.md#oauth-and-authentication).

## Development

```sh
go test -race ./...
go vet ./...
go run ./cmd/docs
go build -o bin/masky ./cmd/masky
python3 scripts/release.py v0.1.0
```

[Plan and architecture](docs/PLAN.md) · [API differences](docs/API-NOTES.md) ·
[Validation report](docs/VALIDATION.md) · [Contributing](CONTRIBUTING.md) ·
[Security](SECURITY.md)

MIT-licensed CLI. The upstream specification is separately attributed in
[NOTICE](NOTICE). No Masky account data or live credentials are included.
