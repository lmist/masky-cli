package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/lmist/masky-cli/internal/secureio"
	"github.com/lmist/masky-cli/internal/spec"
	"github.com/spf13/cobra"
)

func decodeJSON(b []byte) (any, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, usage("invalid JSON input")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, usage("expected exactly one JSON value")
	}
	return v, nil
}
func readInput(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return secureio.Read(cmd.InOrStdin(), 1<<20)
	}
	return secureio.ReadFile(path, 1<<20)
}
func readObject(cmd *cobra.Command, data string) (map[string]any, error) {
	var b []byte
	var err error
	if data == "-" {
		b, err = readInput(cmd, "-")
	} else if strings.HasPrefix(data, "@") {
		b, err = readInput(cmd, strings.TrimPrefix(data, "@"))
	} else {
		b = []byte(data)
	}
	if err != nil {
		return nil, err
	}
	v, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, usage("request body must be a JSON object")
	}
	return m, nil
}
func validate(s *spec.Schema, v any, path string, required bool) error {
	if s == nil {
		return nil
	}
	if v == nil {
		if s.AllowsNull() {
			return nil
		}
		return usage("%s cannot be null", path)
	}
	switch s.Kind() {
	case "string":
		text, ok := v.(string)
		if !ok {
			return usage("%s must be a string", path)
		}
		if required && strings.TrimSpace(text) == "" && !s.AllowsNull() {
			return usage("%s cannot be empty", path)
		}
		if s.MaxLength > 0 && utf8.RuneCountInString(text) > s.MaxLength {
			return usage("%s exceeds %d characters", path, s.MaxLength)
		}
		if s.Pattern != "" {
			ok, err := regexp.MatchString(s.Pattern, text)
			if err != nil {
				return err
			}
			if !ok {
				return usage("%s does not match the documented format", path)
			}
		}
		if s.Format == "uri" && !httpURL(text) {
			return usage("%s must be an HTTP(S) URL", path)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return usage("%s must be a boolean", path)
		}
	case "number", "integer":
		n, ok := number(v)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return usage("%s must be a number", path)
		}
		if s.Kind() == "integer" && math.Trunc(n) != n {
			return usage("%s must be an integer", path)
		}
		if s.Minimum != nil && n < *s.Minimum {
			return usage("%s must be at least %g", path, *s.Minimum)
		}
		if s.Maximum != nil && n > *s.Maximum {
			return usage("%s must be at most %g", path, *s.Maximum)
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return usage("%s must be an array", path)
		}
		if s.MinItems > 0 && len(list) < s.MinItems {
			return usage("%s requires at least %d items", path, s.MinItems)
		}
		if s.MaxItems > 0 && len(list) > s.MaxItems {
			return usage("%s accepts at most %d items", path, s.MaxItems)
		}
		for i, item := range list {
			if err := validate(s.Items, item, fmt.Sprintf("%s[%d]", path, i), false); err != nil {
				return err
			}
		}
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return usage("%s must be an object", path)
		}
		for _, key := range s.Required {
			if _, ok := m[key]; !ok {
				return usage("missing required field %s%s", prefix(path), key)
			}
		}
		for key, value := range m {
			if prop, ok := s.Properties[key]; ok {
				if err := validate(prop, value, prefix(path)+key, slices.Contains(s.Required, key)); err != nil {
					return err
				}
			}
		}
	}
	if len(s.Enum) > 0 {
		matched := false
		for _, allowed := range s.Enum {
			if fmt.Sprint(v) == fmt.Sprint(allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return usage("%s must be one of %v", path, s.Enum)
		}
	}
	return nil
}
func prefix(s string) string {
	if s == "" {
		return ""
	}
	return s + "."
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
func httpURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
func has(m map[string]any, k string) bool   { v, ok := m[k]; return ok && v != nil && v != "" }
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func semantic(e spec.Endpoint, body map[string]any) error {
	for _, key := range []string{"imageUrl", "avatarImageUrl", "lastFrameImage", "audio"} {
		if prop := e.Body.Properties[key]; prop != nil && prop.Kind() != "string" {
			continue
		}
		if has(body, key) && !httpURL(str(body, key)) {
			return usage("%s must be an HTTP(S) URL", key)
		}
	}
	for _, key := range []string{"images", "knowledgeBaseUrls"} {
		if list, ok := body[key].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); !ok || !httpURL(s) {
					return usage("%s must contain HTTP(S) URLs", key)
				}
			}
		}
	}
	switch e.Command {
	case "conversations translate":
		if !has(body, "cc") && !has(body, "audio") && !has(body, "video") {
			return usage("provide at least one of --cc, --audio, or --video")
		}
	case "avatars brain recall":
		if v, ok := body["limit"]; ok {
			n, _ := number(v)
			if n < 1 {
				return usage("limit must be positive")
			}
		}
	case "images edit":
		if has(body, "imageUrl") == has(body, "images") {
			return usage("provide exactly one of --image-url or --images")
		}
		if list, ok := body["images"].([]any); ok && str(body, "model") == "qwen" && len(list) > 2 {
			return usage("qwen accepts at most 2 reference images")
		}
	case "videos generate":
		if has(body, "speechText") && has(body, "audio") {
			return usage("--speech-text and --audio are mutually exclusive")
		}
		if has(body, "speechText") && !has(body, "avatarId") {
			return usage("--speech-text requires --avatar-id")
		}
	case "oauth token":
		if str(body, "grant_type") == "authorization_code" {
			if !has(body, "code") || !has(body, "redirect_uri") {
				return usage("authorization_code requires code and redirect_uri")
			}
			if !has(body, "client_secret") && !has(body, "code_verifier") {
				return usage("authorization_code requires client_secret or code_verifier")
			}
		} else if !has(body, "client_secret") {
			return usage("client_credentials requires client_secret (or MASKY_CLIENT_SECRET)")
		}
		if has(body, "redirect_uri") {
			if err := validateRedirect(str(body, "redirect_uri")); err != nil {
				return err
			}
		}
	case "oauth clients create":
		if domains, ok := body["redirectDomains"].([]any); ok {
			for _, d := range domains {
				s := fmt.Sprint(d)
				u, err := url.Parse("https://" + s)
				if err != nil || u.Hostname() == "" || u.Host != s || u.User != nil || strings.ContainsAny(s, "/?# ") {
					return usage("redirectDomains must contain hosts, not URLs")
				}
			}
		}
	case "conversations turn":
		if has(body, "userOutput") && !has(body, "speakerAvatarId") {
			return usage("--user-output requires --speaker-avatar-id")
		}
	}
	return nil
}
