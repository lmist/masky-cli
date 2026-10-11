package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lmist/masky-cli/internal/config"
)

func TestVideoWaitAndDownload(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	var gets, posts atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/videos/generate":
			posts.Add(1)
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"generationId":"g1","status":"queued"}`)
		case "/api/videos/g1":
			if gets.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"generationId":"g1","status":"rendering"}`)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"generationId": "g1", "status": "ready", "videoUrl": server.URL + "/media"})
			}
		case "/media":
			if r.Header.Get("Authorization") != "" {
				t.Error("media received token")
			}
			_, _ = io.WriteString(w, "video bytes")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "clip.mp4")
	code, out, errout := runCLI(t, "", "videos", "generate", "--prompt", "test", "--base-url", server.URL+"/api", "--download", path, "--poll-interval", "1ms", "--wait-timeout", "1s")
	if code != 0 {
		t.Fatal(errout)
	}
	if posts.Load() != 1 || gets.Load() != 2 {
		t.Fatal("wrong lifecycle")
	}
	if !strings.Contains(out, `"ready"`) || strings.Contains(out, "Waiting") {
		t.Fatalf("pollution in stdout %s", out)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "video bytes" {
		t.Fatal("download failed")
	}
}
func TestSpeakDoesNotFinishVideoAtAudio(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	var gets atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := gets.Add(1)
		if n < 3 {
			_, _ = io.WriteString(w, `{"generation":{"id":"g1","status":"audio","output":"video","audioUrl":"https://x.test/audio"}}`)
		} else {
			_, _ = io.WriteString(w, `{"generation":{"id":"g1","status":"video","output":"video","audioUrl":"https://x.test/audio","videoUrl":"https://x.test/video"}}`)
		}
	}))
	defer s.Close()
	code, out, errout := runCLI(t, "", "speak", "get", "g1", "--wait", "--base-url", s.URL, "--poll-interval", "1ms")
	if code != 0 {
		t.Fatal(errout)
	}
	if gets.Load() != 3 || !strings.Contains(out, "videoUrl") {
		t.Fatalf("finished too early: %d %s", gets.Load(), out)
	}
}
func TestTurnWaitSelectsExactTurn(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	var gets atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"turn":{"id":"target","status":"pending","output":"text"}}`)
			return
		}
		status := "pending"
		if gets.Add(1) > 1 {
			status = "ready"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"turns": []any{map[string]any{"id": "unrelated", "status": "ready", "output": "text", "avatarText": "wrong"}, map[string]any{"id": "target", "status": status, "output": "text", "avatarText": "right"}}})
	}))
	defer s.Close()
	code, out, errout := runCLI(t, "", "conversations", "turn", "c-26-10-ab12", "--user-text", "Hi", "--output", "text", "--wait", "--base-url", s.URL, "--poll-interval", "1ms")
	if code != 0 {
		t.Fatal(errout)
	}
	if strings.Contains(out, "wrong") || !strings.Contains(out, "right") || gets.Load() != 2 {
		t.Fatal(out)
	}
}
func TestWaitFailureTimeoutAndCancellation(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	for _, tc := range []struct {
		body string
		code int
	}{{`{"status":"error","error":"upstream failed"}`, 7}, {`{"status":"queued"}`, 6}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
		code, _, errout := runCLI(t, "", "videos", "wait", "g1", "--base-url", s.URL, "--poll-interval", "1ms", "--wait-timeout", "10ms")
		s.Close()
		if code != tc.code {
			t.Fatalf("exit=%d wanted %d: %s", code, tc.code, errout)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errout bytes.Buffer
	code := Execute(ctx, []string{"videos", "wait", "g1"}, strings.NewReader(""), &out, &errout, "test")
	if code != 130 {
		t.Fatalf("cancel exit %d %s", code, errout.String())
	}
}
func TestDonationTerminalStates(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	for _, status := range []string{"confirmed", "cancelled", "expired"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"intentId": "i1", "status": status})
		}))
		code, out, errout := runCLI(t, "", "donations", "wait", "i1", "--base-url", s.URL)
		s.Close()
		if code != 0 || !strings.Contains(out, status) {
			t.Fatalf("terminal state: %s %s", out, errout)
		}
	}
}
func TestAuthProfilesAndStatus(t *testing.T) {
	cleanEnv(t)
	code, out, errout := runCLI(t, "private-fixture\n", "auth", "login", "--profile", "work", "--token-stdin")
	if code != 0 || strings.Contains(out, "private-fixture") {
		t.Fatalf("login %s %s", out, errout)
	}
	c, err := config.Load(os.Getenv("MASKY_CONFIG"))
	if err != nil || c.Profiles["work"].Token != "private-fixture" {
		t.Fatal("token not saved")
	}
	code, out, errout = runCLI(t, "", "auth", "status")
	if code != 0 || strings.Contains(out, "private-fixture") || !strings.Contains(out, `"credentialSource": "profile"`) {
		t.Fatalf("status %s %s", out, errout)
	}
	code, out, errout = runCLI(t, "", "auth", "profiles")
	if code != 0 || !strings.Contains(out, "work") {
		t.Fatalf("profiles %s %s", out, errout)
	}
	code, _, errout = runCLI(t, "", "auth", "logout")
	if code != 0 {
		t.Fatal(errout)
	}
	c, err = config.Load(os.Getenv("MASKY_CONFIG"))
	if err != nil || c.Profiles["work"].Token != "" {
		t.Fatal("logout did not clear token")
	}
}
func TestOAuthPKCEFlowAndStateRejection(t *testing.T) {
	cleanEnv(t)
	var requests atomic.Int32
	var expected oauthSession
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("OAuth exchange sent bearer")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["code_verifier"] != expected.Verifier || body["code"] != "fixture-code" || body["grant_type"] != "authorization_code" {
			t.Errorf("wrong exchange body: %v", body)
		}
		_, _ = io.WriteString(w, `{"access_token":"fixture-new-token","scope":"profile"}`)
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "session.json")
	code, out, errout := runCLI(t, "", "oauth", "authorize", "--client-id", "client1", "--redirect-uri", "http://localhost:8910/callback", "--session", path, "--base-url", s.URL+"/api")
	if code != 0 {
		t.Fatal(errout)
	}
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &expected) != nil {
		t.Fatal("session unreadable")
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("session is not private")
	}
	var response map[string]any
	_ = json.Unmarshal([]byte(out), &response)
	u, _ := url.Parse(response["url"].(string))
	if u.Query().Get("code_challenge") != challenge(expected.Verifier) || u.Query().Get("state") != expected.State || strings.Contains(out, expected.Verifier) {
		t.Fatal("invalid PKCE output")
	}
	for _, callback := range []string{"http://localhost:8910/callback?code=fixture-code&state=wrong", "https://evil.test/callback?code=fixture-code&state=" + expected.State} {
		code, _, _ = runCLI(t, "", "oauth", "exchange", "--session", path, "--callback-url", callback, "--base-url", s.URL+"/api")
		if code != 2 {
			t.Fatal("invalid callback accepted")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid callback made request")
	}
	callback := "http://localhost:8910/callback?code=fixture-code&state=" + expected.State
	code, out, errout = runCLI(t, "", "oauth", "exchange", "--session", path, "--callback-url", callback, "--base-url", s.URL+"/api", "--store")
	if code != 0 {
		t.Fatal(errout)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("consumed session retained")
	}
	c, err := config.Load(os.Getenv("MASKY_CONFIG"))
	if err != nil || c.Profiles["default"].Token != "fixture-new-token" {
		t.Fatal("token not stored")
	}
}
func TestPKCERFCVector(t *testing.T) {
	if got := challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal(got)
	}
}
func TestOAuthExpiredSession(t *testing.T) {
	cleanEnv(t)
	path := filepath.Join(t.TempDir(), "session")
	b, _ := json.Marshal(oauthSession{ClientID: "id", State: "state", Verifier: "verifier", CreatedAt: time.Now().Add(-time.Hour)})
	_ = os.WriteFile(path, b, 0600)
	code, _, errout := runCLI(t, "", "oauth", "exchange", "--session", path, "--callback-url", "http://localhost/cb")
	if code != 2 || !strings.Contains(errout, "expired") {
		t.Fatal(errout)
	}
}
func TestOutputSelectionAndPrivateSave(t *testing.T) {
	cleanEnv(t)
	path := filepath.Join(t.TempDir(), "out.json")
	code, out, errout := runCLI(t, "", "version", "--save-json", path)
	if code != 0 || out != "" {
		t.Fatalf("save %s %s", out, errout)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("output is not private")
	}
	code, _, _ = runCLI(t, "", "version", "--save-json", path)
	if code == 0 {
		t.Fatal("overwrote output")
	}
	code, out, errout = runCLI(t, "", "version", "--select", "version", "--raw")
	if code != 0 || out != "test\n" {
		t.Fatalf("select %s %s", out, errout)
	}
	code, _, _ = runCLI(t, "", "version", "--select", "missing")
	if code != 2 {
		t.Fatal("missing selection accepted")
	}
	value, err := selectValue(map[string]any{"items": []any{map[string]any{"id": "a"}}}, "items.0.id")
	if err != nil || value != "a" {
		t.Fatal("array selection failed")
	}
	var table bytes.Buffer
	if err := writeTable(&table, map[string]any{"avatars": []any{map[string]any{"id": "a", "name": "\x1b[31mbad\nname"}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(table.String(), "\x1b") {
		t.Fatal("terminal control escaped into table")
	}
}
func TestShareQueryAndEscaping(t *testing.T) {
	cleanEnv(t)
	file := filepath.Join(t.TempDir(), "viewer")
	_ = os.WriteFile(file, []byte("private&value"), 0600)
	code, out, errout := runCLI(t, "", "share", "live", "c-26-10-ab12", "--viewer-token-file", file, "--since", "now", "--paused", "--iframe")
	if code != 0 {
		t.Fatal(errout)
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(out), &result)
	u, _ := url.Parse(result["url"].(string))
	if u.Query().Get("token") != "private&value" || u.Query().Get("since") != "now" || u.Query().Get("autoplay") != "0" {
		t.Fatal(out)
	}
	if !strings.Contains(result["iframe"].(string), "&amp;") {
		t.Fatal("iframe attributes not escaped")
	}
}
func TestCompletionScripts(t *testing.T) {
	cleanEnv(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		code, out, errout := runCLI(t, "", "completion", shell)
		if code != 0 || len(out) < 100 {
			t.Fatalf("%s completion failed: %s", shell, errout)
		}
	}
}

func TestExistingDownloadStopsBeforeBilling(t *testing.T) {
	cleanEnv(t)
	t.Setenv("MASKY_API_KEY", "fixture")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("billed request made before output preflight") }))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "existing")
	_ = os.WriteFile(path, []byte("original"), 0600)
	code, _, errout := runCLI(t, "", "images", "generate", "--prompt", "A circle", "--download", path, "--base-url", s.URL)
	if code == 0 || !strings.Contains(errout, "already exists") {
		t.Fatalf("preflight failed: %s", errout)
	}
}

func TestFallbackAndVideoFailure(t *testing.T) {
	ready, err := jobReady(map[string]any{"status": "ready", "output": nil, "avatarText": "Fallback text"}, "turn", "video")
	if !ready || err != nil {
		t.Fatal("voice-less fallback did not finish")
	}
	_, err = jobReady(map[string]any{"status": "audio", "output": "video", "audioUrl": "https://example.com/a", "videoError": "render failed"}, "speak", "video")
	if ExitCode(err) != 7 {
		t.Fatal("video failure was not terminal")
	}
}
