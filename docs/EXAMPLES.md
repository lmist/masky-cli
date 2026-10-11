# Examples

These examples use placeholders. Generation consumes Masky credits; inspect
`masky videos pricing` and the current API docs for rates.

## Image composition and text

```sh
masky images generate --prompt 'A sign reading "Good Morning"' --model ideogram
masky images edit --prompt 'Put the person in front of the building' \
  --images https://example.com/person.jpg --images https://example.com/building.jpg
masky images edit --prompt 'Change the sign to "Open"' \
  --image-url https://example.com/sign.jpg --model qwen
```

## Video inputs

```sh
# Force text-to-video even when an avatar is specified.
masky videos generate --prompt 'A cinematic sunrise' --set image=null --duration 5

# Animate one owned avatar using a chosen opening frame.
masky videos generate --prompt 'Wave to the viewer' --avatar-id AVATAR_ID \
  --image https://example.com/frame.jpg --resolution 720p --fps 24 --wait

# Speech audio uses the owned avatar's voice and determines the video length.
masky videos generate --prompt 'Walk through a garden' --avatar-id AVATAR_ID \
  --speech-text 'Welcome to the garden.' --wait
```

Scene generation is not lip-sync. Use `avatars speak --output video` or a
conversation speak turn for a talking head with synchronized lips.

## Avatar variants, stream identity, and memory

```sh
masky avatars images add AVATAR_ID --image-url https://example.com/look.jpg
masky avatars images list AVATAR_ID
masky avatars stream-identity set --avatar-id AVATAR_ID
masky avatars stream-identity set --set avatarId=null
masky avatars set-public AVATAR_ID --publicly-renderable=false
masky avatars set-voice AVATAR_ID --voice-id VOICE_ID
masky avatars train AVATAR_ID

masky avatars brain save AVATAR_ID --content 'Prefers metric units.' \
  --sandbox-id session_123
masky avatars brain recall AVATAR_ID --query 'Unit preference?' \
  --sandbox-id session_123 --limit 8
masky avatars brain query AVATAR_ID --query 'What do you remember?' \
  --sandbox-id session_123
masky avatars brain clear-sandbox AVATAR_ID session_123 --yes
```

A brain must already be configured in the Masky UI. Recall works across the
documented memory systems; synthesized query requires Masky-managed memory and
the brain's LLM key.

## Two-avatar exchanges

```sh
masky conversations turn c-26-10-ab12 \
  --speaker-avatar-id GUEST_AVATAR_ID \
  --user-text 'What is espresso?' \
  --avatar-text 'Coffee brewed under pressure.' \
  --user-output video --output video --wait
```

The conversation avatar speaks `avatarText`; the second owned avatar speaks
`userText`. Waiting follows the returned avatar turn, not the separate user turn
or any follow-on chunks. `conversations get` includes the complete transcript.

## Sharing and embedding

```sh
masky share url c-26-10-ab12
masky share live c-26-10-ab12 --viewer-token-file viewer-token.txt \
  --since now --paused --iframe --width 640 --height 360
masky share live t-26-10-ab12 --iframe
masky share resolve g-26-10-ab12
masky conversations access-link CONVERSATION_ID
```

Live viewer URLs are constructed with query encoding, and iframe attributes are
HTML-escaped. The `viewer-token.txt` file contains the viewer token, not your API
key. URL construction does not check whether a private conversation requires a
token; use the API-returned `liveUrl` when possible. Resolve never sends a bearer.

## OAuth and authentication

Register an app using a first-party API key:

```sh
masky oauth clients create --name 'My App' \
  --redirect-domains localhost --scopes profile --scopes avatars:read \
  --save-json client.json

masky oauth authorize --client-id CLIENT_ID \
  --redirect-uri http://localhost:8910/callback \
  --scope profile --scope avatars:read --session oauth-session.json
```

Open the returned URL, approve in Masky, and copy the complete redirect URL from
the address bar. The CLI does not start a callback server; a localhost page may
fail to load, but its address still contains the code and state.

```sh
masky oauth exchange --session oauth-session.json \
  --callback-url 'http://localhost:8910/callback?code=CODE&state=STATE' \
  --profile default --store --save-json token.json
masky oauth userinfo
```

The callback must exactly match the stored scheme, host, and path, and its state
must match the generated state. `--store` persists the token; omit it to use
stdout or the saved response yourself. Successful exchange removes the session.
Use a fresh named profile created via `auth login` if you want to preserve an
existing first-party profile.

For service identities, attach one of your own avatars and provide the client
secret through `MASKY_CLIENT_SECRET` (for example, injected by a secret manager):

```sh
masky oauth clients set-service-avatar CLIENT_ID --service-avatar-id AVATAR_ID
masky oauth token --grant-type client_credentials --client-id CLIENT_ID \
  --scope profile --save-json service-token.json
masky oauth clients set-service-avatar CLIENT_ID --set serviceAvatarId=null
```

Direct authorization-code exchange is also available through `oauth token` with
`--grant-type authorization_code --code ... --redirect-uri ... --client-id ...`
and either `MASKY_CLIENT_SECRET` or `MASKY_CODE_VERIFIER`. Unlike `oauth exchange`,
this low-level command leaves callback/state validation to your application.

## Tribes, gifts, and restricted credentials

```sh
masky tribes get CREATOR
masky tribes membership CREATOR --member VIEWER
masky tribes join CREATOR --dry-run
masky tribes join CREATOR --yes

masky donations create --to CREATOR --amount 0.5 --note 'Thanks!'
masky donations get INTENT_ID
masky donations wait INTENT_ID
masky donations cancel INTENT_ID
masky donations sent --to CREATOR
```

To send a gift, open the returned `confirmUrl` and confirm in Masky. An API key or
SSO token cannot execute the transfer. `donations confirm INTENT_ID --yes` is a
first-party integration command requiring `MASKY_SESSION_TOKEN` with a Firebase
session, as do `keys create` and `avatars agent-test`.

`donations wait` stops at confirmed, cancelled, or expired; inspect its status
to distinguish a completed transfer from another terminal outcome.

## Signed local-agent reply

```sh
masky conversations agent-complete CONVERSATION_ID TURN_ID \
  --text-file reply.txt --dry-run

# Sign the returned URL, POST method, and bodySha256 in a NIP-98 kind-27235
# event with the bound agent key. Save 'Nostr BASE64_EVENT' to nostr-auth.txt.
masky conversations agent-complete CONVERSATION_ID TURN_ID \
  --text-file reply.txt --authorization-file nostr-auth.txt
```

The CLI is an HTTP client; the external agent owns signing and relay listening.
Reuse the exact input file for the signed call so its payload hash is unchanged.
