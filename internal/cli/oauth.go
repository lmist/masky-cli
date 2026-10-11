package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/lmist/masky-cli/internal/config"
	"github.com/lmist/masky-cli/internal/secureio"
	"github.com/spf13/cobra"
)

type oauthSession struct {
	ClientID    string    `json:"clientId"`
	RedirectURI string    `json:"redirectUri"`
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	BaseURL     string    `json:"baseUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func validateRedirect(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return usage("redirect URI must use HTTPS, except localhost, and cannot contain credentials or a fragment")
	}
	return nil
}
func (a *App) addOAuth() {
	g := a.group([]string{"oauth"})
	authorize := &cobra.Command{Use: "authorize", Short: "Create a PKCE consent URL and private session file", Args: noArgs}
	authorize.Flags().String("client-id", "", "Registered OAuth client ID")
	authorize.Flags().String("redirect-uri", "", "Registered callback URI")
	authorize.Flags().StringArray("scope", []string{"profile"}, "Requested scope (repeatable)")
	authorize.Flags().String("session", "", "Private file for state and PKCE verifier (required)")
	authorize.RunE = func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("client-id")
		redirect, _ := cmd.Flags().GetString("redirect-uri")
		path, _ := cmd.Flags().GetString("session")
		scopes, _ := cmd.Flags().GetStringArray("scope")
		if id == "" || redirect == "" || path == "" {
			return usage("--client-id, --redirect-uri, and --session are required")
		}
		if err := validateRedirect(redirect); err != nil {
			return err
		}
		for _, scope := range scopes {
			if !strings.Contains("|profile|avatars:read|generate|tribes|donations|", "|"+scope+"|") || scope == "" {
				return usage("unknown OAuth scope")
			}
		}
		c, err := a.client("none")
		if err != nil {
			return err
		}
		u, _ := url.Parse(c.BaseURL)
		u.Path = "/oauth-authorize.html"
		state, err := randomString()
		if err != nil {
			return err
		}
		verifier, err := randomString()
		if err != nil {
			return err
		}
		q := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "scope": {strings.Join(scopes, " ")}, "state": {state}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}}
		u.RawQuery = q.Encode()
		if a.opts.dryRun {
			return a.output(cmd, map[string]any{"url": u.String(), "session": path, "note": "Dry run: session not saved; do not use this consent URL."})
		}
		session := oauthSession{ClientID: id, RedirectURI: redirect, State: state, Verifier: verifier, BaseURL: c.BaseURL, CreatedAt: time.Now().UTC()}
		if err := secureio.Write(path, a.opts.force, func(w io.Writer) error { return json.NewEncoder(w).Encode(session) }); err != nil {
			return err
		}
		return a.output(cmd, map[string]any{"url": u.String(), "session": path, "next": "Open url, approve, then run masky oauth exchange --session FILE --callback-url URL"})
	}
	g.AddCommand(authorize)
	exchange := &cobra.Command{Use: "exchange", Short: "Validate a PKCE callback and exchange its code for a token", Args: noArgs}
	exchange.Flags().String("session", "", "Session file from oauth authorize")
	exchange.Flags().String("callback-url", "", "Complete callback URL, including code and state")
	exchange.Flags().Bool("store", false, "Store the returned token in the selected profile")
	exchange.RunE = func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("session")
		callback, _ := cmd.Flags().GetString("callback-url")
		store, _ := cmd.Flags().GetBool("store")
		if file == "" || callback == "" {
			return usage("--session and --callback-url are required")
		}
		b, err := secureio.ReadFile(file, 1<<20)
		if err != nil {
			return err
		}
		var session oauthSession
		if json.Unmarshal(b, &session) != nil || session.ClientID == "" || session.State == "" || session.Verifier == "" {
			return usage("invalid OAuth session file")
		}
		if time.Since(session.CreatedAt) > 30*time.Minute || time.Until(session.CreatedAt) > time.Minute {
			return usage("OAuth session expired; run oauth authorize again")
		}
		if err := validateRedirect(session.RedirectURI); err != nil {
			return err
		}
		u, err := url.Parse(callback)
		if err != nil {
			return usage("invalid callback URL")
		}
		expected, _ := url.Parse(session.RedirectURI)
		if u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
			return usage("callback URL does not match the session redirect URI")
		}
		if subtle.ConstantTimeCompare([]byte(u.Query().Get("state")), []byte(session.State)) != 1 {
			return usage("OAuth state mismatch")
		}
		if u.Query().Get("error") != "" {
			return usage("OAuth consent was denied or failed")
		}
		code := u.Query().Get("code")
		if code == "" {
			return usage("callback has no authorization code")
		}
		c, err := a.client("none")
		if err != nil {
			return err
		}
		if c.BaseURL != session.BaseURL {
			return usage("selected API base URL does not match the OAuth session")
		}
		result, err := a.request(cmd, c, api.Request{Method: "POST", Path: "/oauth/token", NoAuth: true, Body: map[string]any{"grant_type": "authorization_code", "client_id": session.ClientID, "redirect_uri": session.RedirectURI, "code": code, "code_verifier": session.Verifier}})
		if err != nil {
			return err
		}
		if a.opts.dryRun {
			return a.output(cmd, result)
		}
		if store {
			token := str(object(result), "access_token")
			if !config.ValidToken(token) {
				return usage("response has no valid access_token")
			}
			path, cfg, name, err := a.load()
			if err != nil {
				return err
			}
			cfg.Profiles[name] = config.Profile{Token: token, BaseURL: c.BaseURL}
			cfg.Current = name
			if err := config.Save(path, cfg); err != nil {
				return err
			}
		}
		if err := a.output(cmd, result); err != nil {
			return err
		}
		return os.Remove(file)
	}
	g.AddCommand(exchange)
}
