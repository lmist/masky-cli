package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lmist/masky-cli/internal/secureio"
)

const MaxResponseBytes = 32 << 20

type Error struct {
	Status    int
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	if e.RequestID != "" {
		s += " (request " + e.RequestID + ")"
	}
	return s
}

type Client struct {
	BaseURL       string
	Token         string
	HTTP          *http.Client
	Retries       int
	AllowInsecure bool
	Version       string
}
type Request struct {
	Method        string
	Path          string
	Query         url.Values
	Body          any
	Authorization string
	NoAuth        bool
}

func ValidateBase(raw string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (allowInsecure || loopback(u.Hostname()))) {
		return nil, errors.New("base URL requires HTTPS (HTTP allowed for localhost or with --allow-insecure)")
	}
	if strings.Contains(u.Path, "..") {
		return nil, errors.New("base URL cannot contain '..'")
	}
	return u, nil
}
func loopback(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
func New(base, token, version string, timeout time.Duration, retries int, allowInsecure bool) (*Client, error) {
	if _, err := ValidateBase(base, allowInsecure); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		return nil, errors.New("--timeout must be positive")
	}
	if retries < 0 || retries > 10 {
		return nil, errors.New("--retries must be between 0 and 10")
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), Token: token, Version: version, Retries: retries, AllowInsecure: allowInsecure, HTTP: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) URL(path string, query url.Values) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\") {
		return "", errors.New("API path must start with one slash and contain no query, fragment, or backslash; use --query")
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return "", errors.New("invalid path escape")
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == ".." || part == "." {
			return "", errors.New("API path cannot contain dot segments")
		}
	}
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u, nil
}
func (c *Client) Do(ctx context.Context, r Request) (any, error) {
	uri, err := c.URL(r.Path, r.Query)
	if err != nil {
		return nil, err
	}
	var body []byte
	if r.Body != nil {
		body, err = json.Marshal(r.Body)
		if err != nil {
			return nil, err
		}
	}
	secrets := append([]string{c.Token, r.Authorization}, secretValues(r.Body)...)
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, r.Method, uri, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("could not construct API request")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "masky-cli/"+c.Version)
		if r.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if r.Authorization != "" {
			req.Header.Set("Authorization", r.Authorization)
		} else if !r.NoAuth && c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, sendErr := c.HTTP.Do(req)
		if sendErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if r.Method == http.MethodGet && attempt < c.Retries {
				if err := sleep(ctx, backoff(attempt)); err != nil {
					return nil, err
				}
				continue
			}
			var hint string
			if r.Method != http.MethodGet {
				hint = "; request was not retried because it may already have succeeded"
			}
			if errors.Is(sendErr, context.DeadlineExceeded) {
				return nil, fmt.Errorf("request timed out%s: %w", hint, context.DeadlineExceeded)
			}
			// url.Error includes request URLs and may include secret query values.
			var ue *url.Error
			if errors.As(sendErr, &ue) {
				sendErr = ue.Err
			}
			return nil, fmt.Errorf("request failed: %s%s", RedactText(sendErr.Error(), secrets...), hint)
		}
		data, readErr := secureio.Read(resp.Body, MaxResponseBytes)
		resp.Body.Close()
		if r.Method == http.MethodGet && attempt < c.Retries && retryable(resp.StatusCode) {
			if err := sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := statusMessage(resp.StatusCode, data)
			message = RedactText(message, secrets...)
			return nil, &Error{Status: resp.StatusCode, Message: message, RequestID: RedactText(resp.Header.Get("X-Request-Id"), secrets...)}
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return map[string]any{}, nil
		}
		var result any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err = dec.Decode(&result); err != nil {
			return nil, errors.New("server returned a non-JSON success response")
		}
		if err = dec.Decode(new(any)); err != io.EOF {
			return nil, errors.New("server returned trailing data after JSON")
		}
		return result, nil
	}
}
func retryable(status int) bool {
	return status == 429 || status == 502 || status == 503 || status == 504
}
func backoff(attempt int) time.Duration {
	d := 250 * time.Millisecond * time.Duration(1<<min(attempt, 5))
	return d
}
func retryDelay(value string, attempt int) time.Duration {
	if n, err := strconv.Atoi(value); err == nil && n >= 0 {
		return min(time.Duration(min(n, 10))*time.Second, 10*time.Second)
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, min(time.Until(date), 10*time.Second))
	}
	return backoff(attempt)
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func statusMessage(status int, data []byte) string {
	var result any
	if json.Unmarshal(data, &result) == nil {
		b, _ := json.Marshal(Redact(result))
		return truncate(string(b), 2000)
	}
	// HTML may include proxy internals; do not echo arbitrary response bodies.
	return http.StatusText(status)
}
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var keyPattern = regexp.MustCompile(`(?i)\b(?:mky|mks)_[a-z0-9_-]+`)
var bearerPattern = regexp.MustCompile(`(?i)(bearer|nostr)\s+[a-z0-9_+/=.:-]+`)
var querySecret = regexp.MustCompile(`(?i)([?&](?:token|access_token|key|signature|x-goog-signature)=)[^\s&"<>]+`)
var jwtPattern = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

func RedactText(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	s = keyPattern.ReplaceAllString(s, "[REDACTED]")
	s = bearerPattern.ReplaceAllString(s, "$1 [REDACTED]")
	s = jwtPattern.ReplaceAllString(s, "[REDACTED]")
	return querySecret.ReplaceAllString(s, "${1}[REDACTED]")
}
func Redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			normalized := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			switch normalized {
			case "token", "accesstoken", "refreshtoken", "clientsecret", "codeverifier", "authorization", "viewertoken", "code":
				out[k] = "[REDACTED]"
			default:
				out[k] = Redact(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = Redact(value)
		}
		return out
	case string:
		return RedactText(x)
	default:
		return v
	}
}

func secretValues(v any) []string {
	var result []string
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			n := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			switch n {
			case "token", "accesstoken", "refreshtoken", "clientsecret", "codeverifier", "authorization", "viewertoken", "code":
				if s, ok := value.(string); ok && s != "" {
					result = append(result, s)
				}
			default:
				result = append(result, secretValues(value)...)
			}
		}
	case []any:
		for _, item := range x {
			result = append(result, secretValues(item)...)
		}
	}
	return result
}

// Download never attaches API authorization and only follows HTTP(S) redirects.
func (c *Client) Download(ctx context.Context, raw, path string, force bool) error {
	check := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (c.AllowInsecure || loopback(u.Hostname())))) {
			return errors.New("media URL must use HTTPS (HTTP allowed for localhost or --allow-insecure)")
		}
		return nil
	}
	if err := check(raw); err != nil {
		return err
	}
	h := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many media redirects")
		}
		return check(req.URL.String())
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return errors.New("invalid media URL")
	}
	resp, err := h.Do(req)
	if err != nil {
		return errors.New("media download request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("media download: HTTP %d", resp.StatusCode)
	}
	const maxMedia = 1 << 30
	if resp.ContentLength > maxMedia {
		return errors.New("media exceeds 1 GiB limit")
	}
	return secureio.Write(path, force, func(w io.Writer) error {
		n, err := io.Copy(w, io.LimitReader(resp.Body, maxMedia+1))
		if err != nil {
			return err
		}
		if n > maxMedia {
			return errors.New("media exceeds 1 GiB limit")
		}
		return nil
	})
}
