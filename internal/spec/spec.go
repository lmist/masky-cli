// Package spec maps the pinned public API contract to stable CLI commands.
package spec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:embed openapi.json
var Snapshot []byte

type Schema struct {
	Type        any                `json:"type"`
	Description string             `json:"description"`
	Format      string             `json:"format"`
	Enum        []any              `json:"enum"`
	Default     any                `json:"default"`
	Nullable    bool               `json:"nullable"`
	Required    []string           `json:"required"`
	Properties  map[string]*Schema `json:"properties"`
	Items       *Schema            `json:"items"`
	MaxLength   int                `json:"maxLength"`
	MaxItems    int                `json:"maxItems"`
	MinItems    int                `json:"minItems"`
	Minimum     *float64           `json:"minimum"`
	Maximum     *float64           `json:"maximum"`
	Pattern     string             `json:"pattern"`
}

func (s *Schema) Kind() string {
	if s == nil {
		return ""
	}
	if t, ok := s.Type.(string); ok {
		return t
	}
	if ts, ok := s.Type.([]any); ok {
		for _, t := range ts {
			if t != "null" {
				if v, ok := t.(string); ok {
					return v
				}
			}
		}
	}
	return ""
}
func (s *Schema) AllowsNull() bool {
	if s.Nullable {
		return true
	}
	if ts, ok := s.Type.([]any); ok {
		for _, t := range ts {
			if t == "null" {
				return true
			}
		}
	}
	return false
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required"`
	Description string  `json:"description"`
	Schema      *Schema `json:"schema"`
}
type Operation struct {
	Summary     string      `json:"summary"`
	Parameters  []Parameter `json:"parameters"`
	RequestBody struct {
		Required bool `json:"required"`
		Content  map[string]struct {
			Schema *Schema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type Endpoint struct {
	Command   string
	Method    string
	Path      string
	Summary   string
	Auth      string // bearer, optional, none, session, nostr
	Confirm   bool
	Wait      string // video, speak, turn, donation
	Download  bool
	Source    string
	Operation Operation
	Body      *Schema
}

// Definitions deliberately name every operation. New upstream endpoints make
// the coverage test fail until a reviewed command mapping is added.
var definitions = []Endpoint{
	{Command: "keys create", Method: "POST", Path: "/api-keys", Auth: "session"},
	{Command: "keys list", Method: "GET", Path: "/api-keys", Auth: "bearer"},
	{Command: "keys revoke", Method: "DELETE", Path: "/api-keys/{id}", Auth: "bearer", Confirm: true},
	{Command: "avatars list", Method: "GET", Path: "/avatars", Auth: "bearer"},
	{Command: "avatars create", Method: "POST", Path: "/avatars", Auth: "bearer"},
	{Command: "avatars public", Method: "GET", Path: "/avatars/public", Auth: "optional"},
	{Command: "avatars lookup", Method: "GET", Path: "/avatars/lookup", Auth: "optional"},
	{Command: "avatars stream-identity get", Method: "GET", Path: "/avatars/stream-identity", Auth: "bearer"},
	{Command: "avatars stream-identity set", Method: "PUT", Path: "/avatars/stream-identity", Auth: "bearer"},
	{Command: "avatars set-public", Method: "POST", Path: "/avatars/{avatarId}/public", Auth: "bearer"},
	{Command: "voices list", Method: "GET", Path: "/voices", Auth: "bearer"},
	{Command: "avatars train", Method: "POST", Path: "/avatars/{avatarId}/train", Auth: "bearer"},
	{Command: "avatars set-voice", Method: "POST", Path: "/avatars/{avatarId}/voice", Auth: "bearer"},
	{Command: "conversations create", Method: "POST", Path: "/conversations", Auth: "bearer"},
	{Command: "conversations turn", Method: "POST", Path: "/conversations/{conversationId}/turn", Auth: "bearer", Wait: "turn", Download: true},
	{Command: "conversations set-visibility", Method: "POST", Path: "/conversations/{conversationId}/visibility", Auth: "bearer"},
	{Command: "conversations get", Method: "GET", Path: "/conversations/by-slug/{slug}", Auth: "optional"},
	{Command: "avatars speak", Method: "POST", Path: "/avatars/{avatarId}/speak", Auth: "bearer", Wait: "speak", Download: true},
	{Command: "speak get", Method: "GET", Path: "/avatars/speak/{generationId}", Auth: "bearer", Wait: "speak", Download: true},
	{Command: "images generate", Method: "POST", Path: "/images/generate", Auth: "bearer", Download: true},
	{Command: "images edit", Method: "POST", Path: "/images/edit", Auth: "bearer", Download: true},
	{Command: "oauth clients create", Method: "POST", Path: "/oauth/clients", Auth: "bearer"},
	{Command: "oauth clients list", Method: "GET", Path: "/oauth/clients", Auth: "bearer"},
	{Command: "oauth clients delete", Method: "DELETE", Path: "/oauth/clients/{clientId}", Auth: "bearer", Confirm: true},
	{Command: "oauth clients set-service-avatar", Method: "PATCH", Path: "/oauth/clients/{clientId}", Auth: "bearer"},
	{Command: "oauth token", Method: "POST", Path: "/oauth/token", Auth: "none"},
	{Command: "oauth userinfo", Method: "GET", Path: "/oauth/userinfo", Auth: "bearer"},
	{Command: "oauth grants list", Method: "GET", Path: "/oauth/grants", Auth: "bearer"},
	{Command: "oauth grants revoke", Method: "DELETE", Path: "/oauth/grants/{tokenId}", Auth: "bearer", Confirm: true},
	{Command: "videos generate", Method: "POST", Path: "/videos/generate", Auth: "bearer", Wait: "video", Download: true},
	{Command: "videos get", Method: "GET", Path: "/videos/{generationId}", Auth: "bearer", Wait: "video", Download: true},
	{Command: "tribes get", Method: "GET", Path: "/tribes/{user}", Auth: "bearer"},
	{Command: "tribes membership", Method: "GET", Path: "/tribes/{user}/membership", Auth: "bearer"},
	{Command: "tribes join", Method: "POST", Path: "/tribes/{user}/join", Auth: "bearer", Confirm: true},
	{Command: "donations create", Method: "POST", Path: "/donations/intents", Auth: "bearer", Wait: "donation"},
	{Command: "donations get", Method: "GET", Path: "/donations/intents/{intentId}", Auth: "bearer", Wait: "donation"},
	{Command: "donations confirm", Method: "POST", Path: "/donations/intents/{intentId}/confirm", Auth: "session", Confirm: true},
	{Command: "donations cancel", Method: "POST", Path: "/donations/intents/{intentId}/cancel", Auth: "bearer"},
	{Command: "donations sent", Method: "GET", Path: "/donations/sent", Auth: "bearer"},
	{Command: "avatars images list", Method: "GET", Path: "/avatars/{avatarId}/images", Auth: "bearer"},
	{Command: "avatars images add", Method: "POST", Path: "/avatars/{avatarId}/images", Auth: "bearer"},
	{Command: "conversations set-image", Method: "POST", Path: "/conversations/{conversationId}/avatar-image", Auth: "bearer"},
	{Command: "videos pricing", Method: "GET", Path: "/videos/pricing", Auth: "none"},
	{Command: "conversations agent-complete", Method: "POST", Path: "/conversations/{conversationId}/turns/{turnId}/agent-complete", Auth: "nostr"},
	{Command: "avatars agent-test", Method: "POST", Path: "/avatars/{avatarId}/agent-binding/test", Auth: "session"},
}

func Load() ([]Endpoint, error) {
	var document struct {
		Paths map[string]map[string]Operation `json:"paths"`
	}
	if err := json.Unmarshal(Snapshot, &document); err != nil {
		return nil, err
	}
	result := make([]Endpoint, 0, len(definitions))
	for _, e := range definitions {
		op, ok := document.Paths[e.Path][strings.ToLower(e.Method)]
		if !ok {
			return nil, fmt.Errorf("missing contract for %s %s", e.Method, e.Path)
		}
		e.Operation, e.Summary, e.Source = op, op.Summary, "openapi"
		e.Body = op.RequestBody.Content["application/json"].Schema
		supplement(&e)
		result = append(result, e)
	}
	return append(result, proseEndpoints()...), nil
}

// supplement reconciles explicit statements in the prose documentation with
// stale/missing schema fields. Keep these changes listed in docs/API-NOTES.md.
func supplement(e *Endpoint) {
	if e.Body == nil {
		return
	}
	p := e.Body.Properties
	switch e.Command {
	case "avatars create":
		p["folderId"] = &Schema{Type: "string", Description: "Optional folder from folders list"}
	case "conversations turn":
		p["output"].Enum = []any{"text", "audio", "video"}
		p["speakerAvatarId"] = &Schema{Type: "string", Description: "Second owned avatar that speaks the user line"}
		p["avatarText"] = &Schema{Type: "string", MaxLength: 500, Description: "Exact reply; skips AI generation"}
		p["userOutput"] = &Schema{Type: "string", Enum: []any{"text", "audio", "video"}}
	case "oauth clients create":
		p["scopes"].Items.Enum = []any{"profile", "avatars:read", "generate", "tribes", "donations"}
		p["redirectDomains"].MinItems = 1
	case "images generate":
		p["prompt"].MaxLength = 2000
	case "images edit":
		p["images"].MinItems, p["images"].MaxItems = 1, 5
		p["aspectRatio"].Enum = []any{"match_input_image", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}
	case "donations create":
		min, max := .01, 1000.0
		p["amount"].Minimum, p["amount"].Maximum = &min, &max
	}
}

var placeholders = regexp.MustCompile(`\{([^}]+)\}`)

func (e Endpoint) PathParams() []string {
	var names []string
	for _, m := range placeholders.FindAllStringSubmatch(e.Path, -1) {
		names = append(names, m[1])
	}
	return names
}

var camel = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func Flag(s string) string {
	return strings.ToLower(strings.ReplaceAll(camel.ReplaceAllString(s, "${1}-${2}"), "_", "-"))
}
func Keys(properties map[string]*Schema) []string {
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
