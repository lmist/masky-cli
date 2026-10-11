package spec

// These contracts come from https://masky.ai/masky-api-skill.md (2026-10-12),
// a first-party reference linked by the main docs but newer than OpenAPI.
func proseEndpoints() []Endpoint {
	text := func() *Schema { return &Schema{Type: "string"} }
	boolean := func() *Schema { return &Schema{Type: "boolean"} }
	object := func(required []string, p map[string]*Schema) *Schema {
		return &Schema{Type: "object", Required: required, Properties: p}
	}
	langs := func() *Schema {
		return &Schema{Type: "array", MaxItems: 12, MinItems: 1, Items: &Schema{Type: "string", Enum: []any{"en", "es", "fr", "de", "it", "pt", "ja", "ko", "zh", "ar", "ru", "hi"}}}
	}
	lang := func() *Schema { return langs().Items }
	scope := func(p map[string]*Schema) map[string]*Schema {
		p["speakerRef"] = text()
		p["sandboxId"] = &Schema{Type: "string", Pattern: `^[a-zA-Z0-9_-]{1,64}$`}
		return p
	}
	endpoints := []Endpoint{
		{Command: "avatars rename", Method: "PATCH", Path: "/avatars/{avatarId}", Summary: "Rename an owned avatar", Body: object([]string{"displayName"}, map[string]*Schema{"displayName": text()})},
		{Command: "folders list", Method: "GET", Path: "/avatar-folders", Summary: "List avatar folders"},
		{Command: "folders create", Method: "POST", Path: "/avatar-folders", Summary: "Create an avatar folder", Body: object([]string{"name"}, map[string]*Schema{"name": text(), "icon": {Type: "string", Nullable: true, Format: "uri"}})},
		{Command: "folders delete", Method: "DELETE", Path: "/avatar-folders/{folderId}", Summary: "Delete a folder and unfile its avatars", Confirm: true},
		{Command: "avatars brain save", Method: "POST", Path: "/avatars/{avatarId}/brain/pages", Summary: "Save an avatar brain memory", Body: object([]string{"content"}, scope(map[string]*Schema{"content": text()}))},
		{Command: "avatars brain recall", Method: "POST", Path: "/avatars/{avatarId}/brain/recall", Summary: "Recall ranked brain memories", Body: object([]string{"query"}, scope(map[string]*Schema{"query": text(), "limit": {Type: "integer"}}))},
		{Command: "avatars brain query", Method: "POST", Path: "/avatars/{avatarId}/brain/query", Summary: "Synthesize an answer from Masky-managed brain memory", Body: object([]string{"query"}, scope(map[string]*Schema{"query": text()}))},
		{Command: "avatars brain clear-sandbox", Method: "DELETE", Path: "/avatars/{avatarId}/brain/sandboxes/{sandboxId}", Summary: "Delete all memories in an owned brain sandbox", Confirm: true},
		{Command: "conversations access-link", Method: "POST", Path: "/conversations/{conversationId}/access-link", Summary: "Mint a revocable private conversation access link", Body: object(nil, map[string]*Schema{})},
		{Command: "conversations translate", Method: "POST", Path: "/conversations/{conversationId}/translate", Summary: "Queue caption, audio and video translation tracks", Body: object(nil, map[string]*Schema{"cc": langs(), "audio": langs(), "video": langs()})},
		{Command: "conversations turns translate", Method: "POST", Path: "/conversations/{conversationId}/turns/{turnId}/translate", Summary: "Translate the text of one turn", Body: object([]string{"langs"}, map[string]*Schema{"langs": langs(), "force": boolean()})},
		{Command: "conversations turns set-translation", Method: "PUT", Path: "/conversations/{conversationId}/turns/{turnId}/translation", Summary: "Set a turn's translated text", Body: object([]string{"lang", "text"}, map[string]*Schema{"lang": lang(), "text": text()})},
		{Command: "conversations turns translation-audio", Method: "POST", Path: "/conversations/{conversationId}/turns/{turnId}/translation-audio", Summary: "Generate dubbed audio for a turn", Download: true, Body: object([]string{"lang"}, map[string]*Schema{"lang": lang(), "force": boolean()})},
		{Command: "conversations turns translation-video", Method: "POST", Path: "/conversations/{conversationId}/turns/{turnId}/translation-video", Summary: "Generate a translated lip-synced video for a turn", Download: true, Body: object([]string{"lang"}, map[string]*Schema{"lang": lang(), "force": boolean()})},
	}
	for i := range endpoints {
		endpoints[i].Auth = "bearer"
		endpoints[i].Source = "prose"
	}
	return endpoints
}
