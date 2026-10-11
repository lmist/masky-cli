package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryOpenAPIOperationMapped(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(Snapshot, &doc); err != nil {
		t.Fatal(err)
	}
	endpoints, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	commands := map[string]bool{}
	for _, e := range endpoints {
		key := e.Method + " " + e.Path
		if seen[key] || commands[e.Command] {
			t.Fatalf("duplicate %s", key)
		}
		seen[key] = true
		commands[e.Command] = true
		if e.Source != "openapi" && e.Source != "prose" {
			t.Fatal("missing provenance")
		}
	}
	count := 0
	for path, methods := range doc.Paths {
		for method := range methods {
			if !strings.Contains("|get|post|put|patch|delete|", "|"+method+"|") {
				continue
			}
			count++
			if !seen[strings.ToUpper(method)+" "+path] {
				t.Errorf("unmapped %s %s", method, path)
			}
		}
	}
	if count != 45 || len(endpoints) != 59 {
		t.Fatalf("coverage changed: %d OpenAPI, %d total; update docs and tests", count, len(endpoints))
	}
}
