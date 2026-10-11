package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/lmist/masky-cli/internal/config"
	"github.com/spf13/cobra"
)

func (a *App) addAuth() {
	g := a.group([]string{"auth"})
	login := &cobra.Command{Use: "login", Short: "Store a token privately in the selected profile", Args: noArgs}
	login.Flags().Bool("token-stdin", false, "Read the token from stdin")
	login.Flags().String("token-file", "", "Read the token from a file")
	login.RunE = func(cmd *cobra.Command, args []string) error {
		stdin, _ := cmd.Flags().GetBool("token-stdin")
		file, _ := cmd.Flags().GetString("token-file")
		if stdin == (file != "") {
			return usage("provide exactly one of --token-stdin or --token-file")
		}
		if a.opts.dryRun {
			return usage("--dry-run applies to HTTP requests; auth login changes local credential storage")
		}
		if stdin {
			file = "-"
		}
		b, err := readInput(cmd, file)
		if err != nil {
			return err
		}
		token := strings.TrimSpace(string(b))
		if !config.ValidToken(token) {
			return usage("token must be nonempty and contain no whitespace")
		}
		path, c, name, err := a.load()
		if err != nil {
			return err
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`).MatchString(name) {
			return usage("profile name must use 1–64 letters, digits, hyphens, or underscores")
		}
		base := c.Profiles[name].BaseURL
		if base == "" {
			base = config.DefaultBaseURL
		}
		if env := os.Getenv("MASKY_BASE_URL"); env != "" {
			base = env
		}
		if a.opts.base != "" {
			base = a.opts.base
		}
		base = strings.TrimRight(base, "/")
		if _, err := api.ValidateBase(base, a.opts.allowInsecure); err != nil {
			return usage("%s", err)
		}
		c.Profiles[name] = config.Profile{Token: token, BaseURL: base}
		c.Current = name
		if err := config.Save(path, c); err != nil {
			return err
		}
		return a.output(cmd, map[string]any{"profile": name, "baseUrl": base, "authenticated": true, "config": path})
	}
	g.AddCommand(login)
	status := &cobra.Command{Use: "status", Short: "Show credential source and profile, without revealing the token", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		path, c, name, err := a.load()
		if err != nil {
			return err
		}
		base, token, source, err := config.Resolve(c, name, a.opts.base)
		if err != nil {
			return err
		}
		return a.output(cmd, map[string]any{"profile": name, "baseUrl": base, "authenticated": token != "", "credentialSource": source, "config": path, "note": "Local status only; use avatars list to verify the credential with Masky."})
	}}
	g.AddCommand(status)
	logout := &cobra.Command{Use: "logout", Short: "Remove the stored token from the selected profile", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if a.opts.dryRun {
			return usage("--dry-run applies to HTTP requests; logout changes local credential storage")
		}
		path, c, name, err := a.load()
		if err != nil {
			return err
		}
		p := c.Profiles[name]
		p.Token = ""
		c.Profiles[name] = p
		if err := config.Save(path, c); err != nil {
			return err
		}
		if os.Getenv("MASKY_API_KEY") != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "MASKY_API_KEY is still set; unset it to remove the environment credential.")
		}
		return a.output(cmd, map[string]any{"profile": name, "storedTokenRemoved": true})
	}}
	g.AddCommand(logout)
	profiles := &cobra.Command{Use: "profiles", Short: "List local profiles without tokens", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		_, c, _, err := a.load()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(c.Profiles))
		for name := range c.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		list := make([]any, 0, len(names))
		for _, name := range names {
			p := c.Profiles[name]
			list = append(list, map[string]any{"name": name, "active": name == c.Current, "baseUrl": p.BaseURL, "authenticated": p.Token != ""})
		}
		return a.output(cmd, map[string]any{"profiles": list})
	}}
	g.AddCommand(profiles)
	use := &cobra.Command{Use: "use <profile>", Short: "Select the default profile", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if a.opts.dryRun {
			return usage("--dry-run applies to HTTP requests; auth use changes local configuration")
		}
		path, c, _, err := a.load()
		if err != nil {
			return err
		}
		if _, ok := c.Profiles[args[0]]; !ok {
			return usage("unknown profile %q", args[0])
		}
		c.Current = args[0]
		if err := config.Save(path, c); err != nil {
			return err
		}
		return a.output(cmd, map[string]any{"profile": args[0]})
	}}
	g.AddCommand(use)
}

func (a *App) addAPI() {
	cmd := &cobra.Command{Use: "api <method> <path>", Short: "Call a relative API path directly", Long: "Call a path relative to the selected API base URL. This escape hatch skips endpoint validation.\nWrites are never retried. DELETE requires --yes. Use --anonymous for public routes.\nExample: masky api GET /avatars", Args: exactArgs(2)}
	cmd.Flags().String("data", "", "JSON object, @file, or - for stdin")
	cmd.Flags().StringArray("query", nil, "Query parameter key=value (repeatable)")
	cmd.Flags().Bool("anonymous", false, "Do not send authorization")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		method := strings.ToUpper(args[0])
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return usage("method must be GET, POST, PUT, PATCH, or DELETE")
		}
		if method == "DELETE" && !a.opts.yes && !a.opts.dryRun {
			return usage("DELETE requires --yes")
		}
		anonymous, _ := cmd.Flags().GetBool("anonymous")
		auth := "bearer"
		if anonymous {
			auth = "none"
		}
		c, err := a.client(auth)
		if err != nil {
			return err
		}
		r := api.Request{Method: method, Path: args[1], NoAuth: anonymous}
		if cmd.Flags().Changed("data") {
			if method == "GET" {
				return usage("GET requests cannot have --data")
			}
			data, _ := cmd.Flags().GetString("data")
			r.Body, err = readObject(cmd, data)
			if err != nil {
				return err
			}
		}
		pairs, _ := cmd.Flags().GetStringArray("query")
		r.Query, err = parseQuery(pairs)
		if err != nil {
			return err
		}
		result, err := a.request(cmd, c, r)
		if err != nil {
			return err
		}
		return a.output(cmd, result)
	}
	a.root.AddCommand(cmd)
}
