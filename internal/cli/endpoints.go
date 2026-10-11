package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/lmist/masky-cli/internal/secureio"
	"github.com/lmist/masky-cli/internal/spec"
	"github.com/spf13/cobra"
)

func textFileField(key string) bool {
	return slices.Contains([]string{"prompt", "text", "userText", "avatarText", "speechText", "personalityPrompt", "note", "content", "query"}, key)
}

func fieldFlag(key string) string {
	if key == "force" {
		return "regenerate"
	}
	return spec.Flag(key)
}
func addField(cmd *cobra.Command, key string, s *spec.Schema, required bool) {
	name := fieldFlag(key)
	description := s.Description
	if description == "" {
		description = key
	}
	if required {
		description += " (required)"
	}
	if len(s.Enum) > 0 {
		description += fmt.Sprintf("; values: %v", s.Enum)
	}
	if s.Default != nil {
		description += fmt.Sprintf("; server default: %v", s.Default)
	}
	switch s.Kind() {
	case "boolean":
		cmd.Flags().Bool(name, false, description)
	case "integer":
		cmd.Flags().Int64(name, 0, description)
	case "number":
		cmd.Flags().Float64(name, 0, description)
	case "array":
		if s.Items != nil && s.Items.Kind() == "string" {
			cmd.Flags().StringArray(name, nil, description+" (repeat flag for each item)")
		} else {
			cmd.Flags().String(name, "", description+" (JSON array)")
		}
	default:
		cmd.Flags().String(name, "", description)
	}
	if textFileField(key) {
		cmd.Flags().String(name+"-file", "", "Read "+key+" from a UTF-8 file; - for stdin")
	}
	if s.Kind() == "string" && len(s.Enum) > 0 {
		values := make([]string, 0, len(s.Enum))
		for _, v := range s.Enum {
			values = append(values, fmt.Sprint(v))
		}
		_ = cmd.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
	}
}
func fieldValue(cmd *cobra.Command, key string, s *spec.Schema) (any, bool, error) {
	name := fieldFlag(key)
	changed := cmd.Flags().Changed(name)
	if textFileField(key) && cmd.Flags().Changed(name+"-file") {
		if changed {
			return nil, false, usage("--%s and --%s-file are mutually exclusive", name, name)
		}
		path, _ := cmd.Flags().GetString(name + "-file")
		b, err := readInput(cmd, path)
		return strings.TrimRight(string(b), "\r\n"), true, err
	}
	if !changed {
		return nil, false, nil
	}
	var value any
	var err error
	switch s.Kind() {
	case "boolean":
		value, err = cmd.Flags().GetBool(name)
	case "integer":
		value, err = cmd.Flags().GetInt64(name)
	case "number":
		value, err = cmd.Flags().GetFloat64(name)
	case "array":
		if s.Items != nil && s.Items.Kind() == "string" {
			list, e := cmd.Flags().GetStringArray(name)
			err = e
			items := make([]any, len(list))
			for i, s := range list {
				items[i] = s
			}
			value = items
		} else {
			v, _ := cmd.Flags().GetString(name)
			value, err = decodeJSON([]byte(v))
		}
	case "object":
		v, _ := cmd.Flags().GetString(name)
		value, err = decodeJSON([]byte(v))
	default:
		value, err = cmd.Flags().GetString(name)
	}
	return value, true, err
}
func (a *App) addEndpoint(e spec.Endpoint) {
	words := strings.Fields(e.Command)
	parent := a.group(words[:len(words)-1])
	use := words[len(words)-1]
	for _, p := range e.PathParams() {
		use += " <" + spec.Flag(p) + ">"
	}
	long := e.Summary + "\n\n" + e.Method + " " + e.Path + "\nAuthentication: " + e.Auth + "."
	if e.Auth == "session" {
		long += " Set MASKY_SESSION_TOKEN to a first-party Firebase ID token. API keys cannot be used."
	}
	if e.Auth == "nostr" {
		long += " Provide the complete signed 'Nostr ...' header via --authorization-file. Sign the exact URL, method and JSON body; --dry-run shows the request."
	}
	if e.Confirm {
		long += "\nRequires --yes. Use --dry-run to inspect first."
	}
	if e.Body != nil {
		long += "\n\nMerge order: --data object, explicit flags/files, then --set field=JSON.\nOmitted values are left to server defaults. --set field=null sends an explicit null."
	}
	cmd := &cobra.Command{Use: use, Short: shortSummary(e.Summary), Long: long, Args: exactArgs(len(e.PathParams()))}
	if e.Body != nil {
		cmd.Flags().String("data", "", "JSON object, @file, or - for stdin")
		cmd.Flags().StringArray("set", nil, "Override a body field with JSON: --set image=null")
		for _, key := range spec.Keys(e.Body.Properties) {
			addField(cmd, key, e.Body.Properties[key], slices.Contains(e.Body.Required, key))
		}
	}
	for _, p := range e.Operation.Parameters {
		if p.In == "query" {
			s := *p.Schema
			if s.Description == "" {
				s.Description = p.Description
			}
			addField(cmd, p.Name, &s, p.Required)
		}
	}
	if e.Auth == "nostr" {
		cmd.Flags().String("authorization-file", "", "File containing complete Nostr Authorization header; - for stdin")
	}
	if e.Wait != "" {
		cmd.Flags().Bool("wait", false, "Wait for the requested result to finish")
	}
	if e.Wait == "turn" {
		cmd.Long += "\nWaiting/downloading follows the returned first turn only. Long replies can split into additional turns; use conversations get or the live viewer for the complete reply."
		cmd.Flags().String("slug", "", "Conversation share slug for polling when the positional value is an internal ID")
	}
	if e.Download {
		cmd.Flags().String("download", "", "Download the resulting media to this path (implies --wait for jobs)")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if e.Confirm && !a.opts.yes && !a.opts.dryRun {
			return usage("this operation requires --yes; inspect it first with --dry-run")
		}
		body, err := endpointBody(cmd, e)
		if err != nil {
			return err
		}
		query := url.Values{}
		for _, p := range e.Operation.Parameters {
			if p.In != "query" {
				continue
			}
			v, set, err := fieldValue(cmd, p.Name, p.Schema)
			if err != nil {
				return err
			}
			if !set && p.Required {
				return usage("--%s is required", spec.Flag(p.Name))
			}
			if set {
				if err := validate(p.Schema, v, p.Name, p.Required); err != nil {
					return err
				}
				query.Set(p.Name, fmt.Sprint(v))
			}
		}
		path := e.Path
		for i, p := range e.PathParams() {
			if args[i] == "." || args[i] == ".." || strings.ContainsAny(args[i], "/\\") {
				return usage("invalid %s: identifiers cannot contain slashes or dot segments", p)
			}
			path = strings.ReplaceAll(path, "{"+p+"}", url.PathEscape(args[i]))
		}
		c, err := a.client(e.Auth)
		if err != nil {
			return err
		}
		r := api.Request{Method: e.Method, Path: path, Query: query, NoAuth: e.Auth == "none"}
		if body != nil {
			r.Body = body
		}
		if e.Auth == "nostr" {
			file, _ := cmd.Flags().GetString("authorization-file")
			if file == "" {
				if !a.opts.dryRun {
					return usage("--authorization-file is required")
				}
			} else {
				b, err := readInput(cmd, file)
				if err != nil {
					return err
				}
				r.Authorization = strings.TrimSpace(string(b))
				if !strings.HasPrefix(r.Authorization, "Nostr ") || strings.ContainsAny(r.Authorization, "\r\n") {
					return usage("authorization file must contain one Nostr header")
				}
			}
		}
		wait, _ := cmd.Flags().GetBool("wait")
		download, _ := cmd.Flags().GetString("download")
		if !a.opts.dryRun {
			if err := secureio.CheckOutput(download, a.opts.force); err != nil {
				return err
			}
			if download != "" && a.opts.saveJSON != "" {
				d, _ := filepath.Abs(download)
				s, _ := filepath.Abs(a.opts.saveJSON)
				if d == s {
					return usage("--download and --save-json must use different files")
				}
			}
		}
		wait = wait || (download != "" && e.Wait != "")
		var slug string
		if e.Wait == "turn" && wait {
			slug, _ = cmd.Flags().GetString("slug")
			if strings.HasPrefix(args[0], "c-") {
				slug = args[0]
			}
			if slug == "" {
				return usage("--wait with an internal conversation ID requires --slug; alternatively pass the c-… slug as the argument")
			}
			if err := validSlug(slug, "c"); err != nil {
				return err
			}
		}
		result, err := a.request(cmd, c, r)
		if err != nil {
			return err
		}
		if a.opts.dryRun {
			return a.output(cmd, result)
		}
		if wait {
			result, err = a.waitResult(cmd, c, e.Wait, result, body, args, slug)
			if err != nil {
				return err
			}
		}
		if download != "" {
			media := mediaURL(result, str(body, "output"))
			if media == "" {
				return &JobError{"response does not contain the requested media URL"}
			}
			if err = c.Download(cmd.Context(), media, download, a.opts.force); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Saved media to", download)
		}
		return a.output(cmd, result)
	}
	parent.AddCommand(cmd)
}
func shortSummary(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, " — "); i >= 0 {
		s = s[:i]
	}
	return s
}
func endpointBody(cmd *cobra.Command, e spec.Endpoint) (map[string]any, error) {
	if e.Body == nil {
		return nil, nil
	}
	body := map[string]any{}
	if data, _ := cmd.Flags().GetString("data"); cmd.Flags().Changed("data") {
		var err error
		body, err = readObject(cmd, data)
		if err != nil {
			return nil, err
		}
	}
	for _, key := range spec.Keys(e.Body.Properties) {
		v, set, err := fieldValue(cmd, key, e.Body.Properties[key])
		if err != nil {
			return nil, err
		}
		if set {
			body[key] = v
		}
	}
	sets, _ := cmd.Flags().GetStringArray("set")
	for _, assignment := range sets {
		key, value, ok := strings.Cut(assignment, "=")
		if !ok || key == "" {
			return nil, usage("--set expects field=JSON")
		}
		v, err := decodeJSON([]byte(value))
		if err != nil {
			return nil, err
		}
		body[key] = v
	}
	if e.Command == "oauth token" {
		for key, env := range map[string]string{"client_secret": "MASKY_CLIENT_SECRET", "code_verifier": "MASKY_CODE_VERIFIER"} {
			if _, ok := body[key]; !ok {
				if v := os.Getenv(env); v != "" {
					body[key] = v
				}
			}
		}
	}
	// Normalize flag numbers and arrays to the same representation as JSON input.
	b, err := json.Marshal(body)
	if err != nil {
		return nil, usage("body cannot be encoded as JSON")
	}
	v, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	body = v.(map[string]any)
	if err := validate(e.Body, body, "", false); err != nil {
		return nil, err
	}
	if err := semantic(e, body); err != nil {
		return nil, err
	}
	return body, nil
}
