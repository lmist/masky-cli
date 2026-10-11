package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, base string) *Client {
	t.Helper()
	c, err := New(base, "private-token", "test", time.Second, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestRetriesOnlyGET(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n < 3 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(503)
					_, _ = io.WriteString(w, `{"error":"busy"}`)
				} else {
					_, _ = io.WriteString(w, `{"ok":true}`)
				}
			}))
			defer s.Close()
			_, err := testClient(t, s.URL).Do(context.Background(), Request{Method: method, Path: "/test"})
			want := int32(1)
			if method == "GET" {
				want = 3
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("write unexpectedly retried")
			}
			if calls.Load() != want {
				t.Fatalf("calls %d; want %d", calls.Load(), want)
			}
		})
	}
}
func TestNoCredentialRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	_, err := testClient(t, origin.URL).Do(context.Background(), Request{Method: "GET", Path: "/test"})
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != 302 {
		t.Fatalf("expected redirect error, got %v", err)
	}
	if leaked.Load() {
		t.Fatal("redirect was followed")
	}
}
func TestLimitsAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{{"empty", "", 204, true}, {"valid", `{"id":9007199254740993}`, 200, true}, {"html", "<html>oops</html>", 200, false}, {"trailing", `{} {}`, 200, false}, {"huge", strings.Repeat("x", MaxResponseBytes+1), 200, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			v, err := testClient(t, s.URL).Do(context.Background(), Request{Method: "GET", Path: "/"})
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if tc.name == "valid" {
				if v.(map[string]any)["id"] != json.Number("9007199254740993") {
					t.Fatal("lost integer precision")
				}
			}
		})
	}
}
func TestCancellationAndBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sleep(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, raw := range []string{"999999999", "86400", time.Now().Add(time.Hour).Format(http.TimeFormat)} {
		if d := retryDelay(raw, 0); d > 10*time.Second {
			t.Fatalf("unbounded retry %v", d)
		}
	}
}
func TestDownloadAtomicAndUnauthenticated(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credential forwarded to media server")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/media", 302)
			return
		}
		_, _ = io.WriteString(w, "media content")
	}))
	defer s.Close()
	c := testClient(t, s.URL)
	path := filepath.Join(t.TempDir(), "media.bin")
	if err := c.Download(context.Background(), s.URL+"/redirect", path, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "media content" {
		t.Fatal("wrong content")
	}
	if err := c.Download(context.Background(), s.URL+"/media", path, false); err == nil {
		t.Fatal("overwrote existing output")
	}
	if err := c.Download(context.Background(), s.URL+"/media", path, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Download(context.Background(), "file:///etc/passwd", path, true); err == nil {
		t.Fatal("accepted file URL")
	}
}
func TestDownloadFailurePreservesFile(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "media")
	_ = os.WriteFile(path, []byte("original"), 0600)
	if err := testClient(t, s.URL).Download(context.Background(), s.URL, path, true); err == nil {
		t.Fatal("short read accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("partial download corrupted output")
	}
}
func TestBaseURLValidation(t *testing.T) {
	for _, raw := range []string{"http://remote.test/api", "https://user:pass@masky.ai/api", "https://masky.ai/api?key=x", "file:///api", "//evil.test", "https://masky.ai/a/../api"} {
		if _, err := ValidateBase(raw, false); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"https://masky.ai/api", "http://localhost:8080/api", "http://127.0.0.1:123/api", "http://[::1]:8080/api"} {
		if _, err := ValidateBase(raw, false); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
}
func TestRedaction(t *testing.T) {
	v := map[string]any{"client_secret": "client-private", "nested": []any{map[string]any{"viewerToken": "viewer-private"}}, "message": "Bearer private-value mky_FIXTURE_ONLY https://example.com/?token=secret&x=1"}
	b, _ := json.Marshal(Redact(v))
	for _, secret := range []string{"client-private", "viewer-private", "private-value", "mky_FIXTURE_ONLY", "token=secret"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("leak %s in %s", secret, b)
		}
	}
}

func TestEchoedSecretsRedactedAndTimeoutTyped(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":"rejected sensitive-client-value and sensitive-code-value"}`)
	}))
	defer s.Close()
	c := testClient(t, s.URL)
	c.Retries = 0
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/token", Body: map[string]any{"client_secret": "sensitive-client-value", "code": "sensitive-code-value"}})
	if err == nil || strings.Contains(err.Error(), "sensitive-") {
		t.Fatalf("secret echoed: %v", err)
	}
	c.HTTP.Timeout = 10 * time.Millisecond
	_, err = c.Do(context.Background(), Request{Method: "POST", Path: "/slow"})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "not retried") {
		t.Fatalf("timeout identity lost: %v", err)
	}
}
