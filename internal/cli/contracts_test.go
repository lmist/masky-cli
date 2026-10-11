package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lmist/masky-cli/internal/spec"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MASKY_API_KEY", "MASKY_SESSION_TOKEN", "MASKY_PROFILE", "MASKY_BASE_URL", "MASKY_CLIENT_SECRET", "MASKY_CODE_VERIFIER"} {
		t.Setenv(k, "")
	}
	t.Setenv("MASKY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}
func runCLI(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := Execute(context.Background(), args, strings.NewReader(input), &out, &err, "test")
	return code, out.String(), err.String()
}

type contract struct {
	command, method, path string
	args                  []string
	body, query, auth     string
}

// Fixtures are independently specified expected wire requests, not generated
// from the schemas under test. Every published endpoint must have one.
func contracts() []contract {
	return []contract{
		{"keys create", "POST", "/api-keys", []string{"--name", "test key"}, `{"name":"test key"}`, "", "session"},
		{"keys list", "GET", "/api-keys", nil, "", "", "bearer"},
		{"keys revoke", "DELETE", "/api-keys/key1", []string{"key1", "--yes"}, "", "", "bearer"},
		{"avatars list", "GET", "/avatars", nil, "", "", "bearer"},
		{"avatars create", "POST", "/avatars", []string{"--display-name", "Test", "--image-url", "https://example.com/a.jpg", "--personality-prompt", "Friendly", "--voice-id", "voice1", "--folder-id", "folder1", "--publicly-renderable=false", "--knowledge-base-urls", "https://example.com/info"}, `{"displayName":"Test","imageUrl":"https://example.com/a.jpg","personalityPrompt":"Friendly","voiceId":"voice1","folderId":"folder1","publiclyRenderable":false,"knowledgeBaseUrls":["https://example.com/info"]}`, "", "bearer"},
		{"avatars public", "GET", "/avatars/public", nil, "", "", "bearer"},
		{"avatars lookup", "GET", "/avatars/lookup", []string{"--user", "a+b & c"}, "", "user=a%2Bb+%26+c", "bearer"},
		{"avatars stream-identity get", "GET", "/avatars/stream-identity", nil, "", "", "bearer"},
		{"avatars stream-identity set", "PUT", "/avatars/stream-identity", []string{"--set", "avatarId=null", "--set", "imageUrl=null"}, `{"avatarId":null,"imageUrl":null}`, "", "bearer"},
		{"avatars set-public", "POST", "/avatars/a1/public", []string{"a1", "--publicly-renderable=false"}, `{"publiclyRenderable":false}`, "", "bearer"},
		{"voices list", "GET", "/voices", nil, "", "", "bearer"},
		{"avatars train", "POST", "/avatars/a1/train", []string{"a1"}, "", "", "bearer"},
		{"avatars set-voice", "POST", "/avatars/a1/voice", []string{"a1", "--voice-id", "v1"}, `{"voiceId":"v1"}`, "", "bearer"},
		{"conversations create", "POST", "/conversations", []string{"--avatar-id", "a1", "--avatar-owner-user-id", "owner1"}, `{"avatarId":"a1","avatarOwnerUserId":"owner1"}`, "", "bearer"},
		{"conversations turn", "POST", "/conversations/c1/turn", []string{"c1", "--user-text", "Hi", "--avatar-text", "Hello", "--speaker-avatar-id", "a2", "--user-output", "text", "--output", "text", "--mode", "chat", "--reinterpret=false"}, `{"userText":"Hi","avatarText":"Hello","speakerAvatarId":"a2","userOutput":"text","output":"text","mode":"chat","reinterpret":false}`, "", "bearer"},
		{"conversations set-visibility", "POST", "/conversations/c1/visibility", []string{"c1", "--is-public=false"}, `{"isPublic":false}`, "", "bearer"},
		{"conversations get", "GET", "/conversations/by-slug/c-26-10-ab12", []string{"c-26-10-ab12"}, "", "", "bearer"},
		{"avatars speak", "POST", "/avatars/a1/speak", []string{"a1", "--text", "Hello", "--output", "video", "--text-mode", "personality", "--language", "es", "--avatar-owner-user-id", "u1", "--avatar-image-url", "https://example.com/a.jpg"}, `{"text":"Hello","output":"video","textMode":"personality","language":"es","avatarOwnerUserId":"u1","avatarImageUrl":"https://example.com/a.jpg"}`, "", "bearer"},
		{"speak get", "GET", "/avatars/speak/g1", []string{"g1"}, "", "", "bearer"},
		{"images generate", "POST", "/images/generate", []string{"--prompt", "A bird", "--model", "ideogram", "--aspect-ratio", "16:9"}, `{"prompt":"A bird","model":"ideogram","aspectRatio":"16:9"}`, "", "bearer"},
		{"images edit", "POST", "/images/edit", []string{"--prompt", "Blue", "--images", "https://example.com/a.jpg", "--images", "https://example.com/b.jpg", "--model", "qwen"}, `{"prompt":"Blue","images":["https://example.com/a.jpg","https://example.com/b.jpg"],"model":"qwen"}`, "", "bearer"},
		{"oauth clients create", "POST", "/oauth/clients", []string{"--name", "App", "--redirect-domains", "localhost", "--scopes", "profile", "--scopes", "tribes", "--scopes", "donations", "--service-avatar-id", "a1"}, `{"name":"App","redirectDomains":["localhost"],"scopes":["profile","tribes","donations"],"serviceAvatarId":"a1"}`, "", "bearer"},
		{"oauth clients list", "GET", "/oauth/clients", nil, "", "", "bearer"},
		{"oauth clients delete", "DELETE", "/oauth/clients/client1", []string{"client1", "--yes"}, "", "", "bearer"},
		{"oauth clients set-service-avatar", "PATCH", "/oauth/clients/client1", []string{"client1", "--set", "serviceAvatarId=null"}, `{"serviceAvatarId":null}`, "", "bearer"},
		{"oauth token", "POST", "/oauth/token", []string{"--grant-type", "client_credentials", "--client-id", "client1", "--client-secret", "test-secret", "--scope", "profile"}, `{"grant_type":"client_credentials","client_id":"client1","client_secret":"test-secret","scope":"profile"}`, "", "none"},
		{"oauth userinfo", "GET", "/oauth/userinfo", nil, "", "", "bearer"},
		{"oauth grants list", "GET", "/oauth/grants", nil, "", "", "bearer"},
		{"oauth grants revoke", "DELETE", "/oauth/grants/token1", []string{"token1", "--yes"}, "", "", "bearer"},
		{"videos generate", "POST", "/videos/generate", []string{"--prompt", "Clouds", "--duration", "2", "--resolution", "1080p", "--fps", "48", "--aspect-ratio", "3:4", "--seed", "0", "--draft", "--save-audio=false", "--prompt-upsampling=false", "--visibility", "private", "--set", "image=null", "--last-frame-image", "https://example.com/end.jpg"}, `{"prompt":"Clouds","duration":2,"resolution":"1080p","fps":48,"aspectRatio":"3:4","seed":0,"draft":true,"saveAudio":false,"promptUpsampling":false,"visibility":"private","image":null,"lastFrameImage":"https://example.com/end.jpg"}`, "", "bearer"},
		{"videos get", "GET", "/videos/g1", []string{"g1"}, "", "", "bearer"},
		{"tribes get", "GET", "/tribes/u1", []string{"u1"}, "", "", "bearer"},
		{"tribes membership", "GET", "/tribes/u1/membership", []string{"u1", "--member", "u2"}, "", "member=u2", "bearer"},
		{"tribes join", "POST", "/tribes/u1/join", []string{"u1", "--yes"}, "", "", "bearer"},
		{"donations create", "POST", "/donations/intents", []string{"--to", "u1", "--amount", "0.5", "--note", "Thanks"}, `{"to":"u1","amount":0.5,"note":"Thanks"}`, "", "bearer"},
		{"donations get", "GET", "/donations/intents/i1", []string{"i1"}, "", "", "bearer"},
		{"donations confirm", "POST", "/donations/intents/i1/confirm", []string{"i1", "--yes"}, "", "", "session"},
		{"donations cancel", "POST", "/donations/intents/i1/cancel", []string{"i1"}, "", "", "bearer"},
		{"donations sent", "GET", "/donations/sent", []string{"--to", "u1"}, "", "to=u1", "bearer"},
		{"avatars images list", "GET", "/avatars/a1/images", []string{"a1", "--avatar-owner-user-id", "u1"}, "", "avatarOwnerUserId=u1", "bearer"},
		{"avatars images add", "POST", "/avatars/a1/images", []string{"a1", "--image-url", "https://example.com/a.jpg"}, `{"imageUrl":"https://example.com/a.jpg"}`, "", "bearer"},
		{"conversations set-image", "POST", "/conversations/c1/avatar-image", []string{"c1", "--avatar-image-url", "https://example.com/a.jpg"}, `{"avatarImageUrl":"https://example.com/a.jpg"}`, "", "bearer"},
		{"videos pricing", "GET", "/videos/pricing", nil, "", "", "none"},
		{"conversations agent-complete", "POST", "/conversations/c1/turns/t1/agent-complete", []string{"c1", "t1", "--text", "Reply", "--authorization-file", "-"}, `{"text":"Reply"}`, "", "nostr"},
		{"avatars agent-test", "POST", "/avatars/a1/agent-binding/test", []string{"a1", "--binding", `{"pubkey":"abc","relay":"wss://example.com","kind":9391,"extraTags":[["auth","x"]]}`}, `{"binding":{"pubkey":"abc","relay":"wss://example.com","kind":9391,"extraTags":[["auth","x"]]}}`, "", "session"},
		{"avatars rename", "PATCH", "/avatars/a1", []string{"a1", "--display-name", "Renamed"}, `{"displayName":"Renamed"}`, "", "bearer"},
		{"folders list", "GET", "/avatar-folders", nil, "", "", "bearer"},
		{"folders create", "POST", "/avatar-folders", []string{"--name", "Characters", "--set", "icon=null"}, `{"name":"Characters","icon":null}`, "", "bearer"},
		{"folders delete", "DELETE", "/avatar-folders/f1", []string{"f1", "--yes"}, "", "", "bearer"},
		{"avatars brain save", "POST", "/avatars/a1/brain/pages", []string{"a1", "--content", "Likes coffee", "--speaker-ref", "user1", "--sandbox-id", "session_1"}, `{"content":"Likes coffee","speakerRef":"user1","sandboxId":"session_1"}`, "", "bearer"},
		{"avatars brain recall", "POST", "/avatars/a1/brain/recall", []string{"a1", "--query", "Preferences?", "--limit", "8"}, `{"query":"Preferences?","limit":8}`, "", "bearer"},
		{"avatars brain query", "POST", "/avatars/a1/brain/query", []string{"a1", "--query", "What do you know?"}, `{"query":"What do you know?"}`, "", "bearer"},
		{"avatars brain clear-sandbox", "DELETE", "/avatars/a1/brain/sandboxes/session_1", []string{"a1", "session_1", "--yes"}, "", "", "bearer"},
		{"conversations access-link", "POST", "/conversations/c1/access-link", []string{"c1"}, `{}`, "", "bearer"},
		{"conversations translate", "POST", "/conversations/c1/translate", []string{"c1", "--cc", "es", "--cc", "fr", "--audio", "es", "--video", "es"}, `{"cc":["es","fr"],"audio":["es"],"video":["es"]}`, "", "bearer"},
		{"conversations turns translate", "POST", "/conversations/c1/turns/t1/translate", []string{"c1", "t1", "--langs", "es", "--regenerate"}, `{"langs":["es"],"force":true}`, "", "bearer"},
		{"conversations turns set-translation", "PUT", "/conversations/c1/turns/t1/translation", []string{"c1", "t1", "--lang", "es", "--text", "Hola"}, `{"lang":"es","text":"Hola"}`, "", "bearer"},
		{"conversations turns translation-audio", "POST", "/conversations/c1/turns/t1/translation-audio", []string{"c1", "t1", "--lang", "es"}, `{"lang":"es"}`, "", "bearer"},
		{"conversations turns translation-video", "POST", "/conversations/c1/turns/t1/translation-video", []string{"c1", "t1", "--lang", "es", "--regenerate=false"}, `{"lang":"es","force":false}`, "", "bearer"},
	}
}

func TestAllEndpointContracts(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture-api-key")
	t.Setenv("MASKY_SESSION_TOKEN", "fixture-session")
	seen := map[string]bool{}
	for _, tc := range contracts() {
		t.Run(tc.command, func(t *testing.T) {
			count := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.Method != tc.method || r.URL.Path != "/api"+tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request %s %s; expected %s %s?%s", r.Method, r.URL, tc.method, tc.path, tc.query)
				}
				wantAuth := map[string]string{"bearer": "Bearer fixture-api-key", "session": "Bearer fixture-session", "nostr": "Nostr fixture-event", "none": ""}[tc.auth]
				if r.Header.Get("Authorization") != wantAuth {
					t.Errorf("wrong auth mode")
				}
				b, _ := io.ReadAll(r.Body)
				if tc.body == "" {
					if len(b) != 0 {
						t.Errorf("unexpected body %s", b)
					}
				} else {
					var got, want any
					if json.Unmarshal(b, &got) != nil {
						t.Fatalf("bad JSON body %s", b)
					}
					_ = json.Unmarshal([]byte(tc.body), &want)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body %s; expected %s", b, tc.body)
					}
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON content type")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))
			defer s.Close()
			args := append([]string{"--base-url", s.URL + "/api"}, strings.Fields(tc.command)...)
			args = append(args, tc.args...)
			code, out, errout := runCLI(t, "Nostr fixture-event\n", args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errout)
			}
			if count != 1 {
				t.Errorf("got %d requests", count)
			}
			if !strings.Contains(out, `"ok": true`) {
				t.Errorf("output %s", out)
			}
			seen[tc.command] = true
		})
	}
	endpoints, err := spec.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range endpoints {
		if !seen[e.Command] {
			t.Errorf("missing independent contract fixture for %s", e.Command)
		}
	}
	if len(seen) != len(endpoints) {
		t.Fatal("contract count mismatch")
	}
}

func TestInvalidInputNeverSends(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") }))
	defer s.Close()
	tests := [][]string{
		{"images", "generate"}, {"images", "generate", "--prompt", "x", "--aspect-ratio", "7:5"},
		{"images", "edit", "--prompt", "x"}, {"images", "edit", "--prompt", "x", "--image-url", "ftp://example.com/a"},
		{"images", "edit", "--prompt", "x", "--image-url", "https://example.com/a", "--images", "https://example.com/b"},
		{"images", "edit", "--prompt", "x", "--model", "qwen", "--images", "https://x.test/a", "--images", "https://x.test/b", "--images", "https://x.test/c"},
		{"videos", "generate", "--prompt", "x", "--duration", "21"}, {"videos", "generate", "--prompt", "x", "--fps", "30"},
		{"videos", "generate", "--prompt", "x", "--speech-text", "Hi"}, {"videos", "generate", "--prompt", "x", "--speech-text", "Hi", "--audio", "https://example.com/a", "--avatar-id", "a1"},
		{"videos", "generate", "--data", `{"prompt":"x","duration":1.5}`}, {"videos", "generate", "--data", `{"prompt":"x","draft":"false"}`},
		{"avatars", "set-public", "a1"}, {"avatars", "speak", "a1", "--text", strings.Repeat("a", 501)},
		{"avatars", "create", "--display-name", ""}, {"avatars", "images", "list", "../escape"},
		{"donations", "create", "--to", "u1", "--amount", "0"}, {"donations", "create", "--to", "u1", "--amount", "1001"},
		{"donations", "confirm", "i1", "--yes"}, {"keys", "revoke", "k1"}, {"tribes", "join", "u1"},
		{"oauth", "token", "--grant-type", "client_credentials", "--client-id", "x"},
		{"oauth", "token", "--grant-type", "authorization_code", "--client-id", "x", "--code", "x"},
		{"conversations", "turn", "id1", "--user-text", "Hi", "--wait"},
		{"conversations", "turn", "id1", "--user-text", "Hi", "--user-output", "audio"},
		{"conversations", "translate", "c1"}, {"conversations", "translate", "c1", "--cc", "xx"},
		{"avatars", "brain", "save", "a1", "--content", "Hi", "--sandbox-id", "a/b"},
		{"images", "generate", "--data", `[]`}, {"images", "generate", "--data", `{"prompt":"x"} {}`},
		{"images", "generate", "--prompt", "x", "--set", "broken"}, {"images", "generate", "--prompt", "x", "--set", "prompt=null"},
		{"api", "GET", "https://evil.test"}, {"api", "GET", "//evil.test"}, {"api", "GET", "/../escape"},
		{"api", "GET", "/%2e%2e/escape"}, {"api", "GET", "/avatars?key=secret"}, {"api", "GET", "/avatars", "--data", "{}"},
		{"avatars", "list", "--format", "yaml"}, {"avatars", "list", "--raw"}, {"avatars", "list", "--timeout", "0s"},
		{"avatars", "list", "--retries", "-1"}, {"avatars", "list", "unexpected"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args[:min(3, len(args))], " "), func(t *testing.T) {
			code, _, errout := runCLI(t, "", append([]string{"--base-url", s.URL + "/api"}, args...)...)
			if code == 0 {
				t.Fatalf("unexpected success: %v", args)
			}
			if errout == "" {
				t.Error("missing diagnostic")
			}
		})
	}
}

func TestBodyPrecedenceAndOmission(t *testing.T) {
	cleanEnv(t)
	code, out, err := runCLI(t, `{"prompt":"stdin","draft":true,"saveAudio":true,"future":42}`, "videos", "generate", "--data", "-", "--prompt", "flag", "--draft=false", "--set", "saveAudio=false", "--set", "image=null", "--dry-run")
	if code != 0 {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(out), &result)
	body := result["body"].(map[string]any)
	if body["prompt"] != "flag" || body["draft"] != false || body["saveAudio"] != false || body["future"] != float64(42) {
		t.Fatalf("wrong precedence: %s", out)
	}
	if v, ok := body["image"]; !ok || v != nil {
		t.Fatal("explicit null lost")
	}
	if _, ok := body["resolution"]; ok {
		t.Fatal("omitted server default was sent")
	}
	file := filepath.Join(t.TempDir(), "prompt.txt")
	_ = os.WriteFile(file, []byte("From file\n"), 0600)
	code, out, err = runCLI(t, "", "images", "generate", "--prompt-file", file, "--dry-run")
	if code != 0 || !strings.Contains(out, "From file") {
		t.Fatalf("file input: %s %s", out, err)
	}
}

func TestDryRunAndErrorRedaction(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "mky_FIXTURE_ONLY")
	code, out, err := runCLI(t, "", "oauth", "token", "--grant-type", "client_credentials", "--client-id", "client1", "--client-secret", "private-secret", "--dry-run")
	if code != 0 {
		t.Fatal(err)
	}
	if strings.Contains(out, "private-secret") || strings.Contains(out, "mky_FIXTURE_ONLY") {
		t.Fatal("secret leaked in dry run")
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":"invalid mky_FIXTURE_ONLY","client_secret":"hidden","liveUrl":"https://x.test/?token=viewer-secret"}`)
	}))
	defer s.Close()
	code, out, err = runCLI(t, "", "avatars", "list", "--base-url", s.URL, "--error-format", "json")
	if code != 3 || out != "" {
		t.Fatalf("exit=%d stdout=%s", code, out)
	}
	if strings.Contains(err, "mky_FIXTURE_ONLY") || strings.Contains(err, "viewer-secret") || strings.Contains(err, "hidden") {
		t.Fatalf("secret in error: %s", err)
	}
	if !json.Valid([]byte(err)) {
		t.Fatalf("not JSON: %s", err)
	}
}
