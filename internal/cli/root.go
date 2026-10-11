// Package cli assembles the user interface and higher-level workflows.
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/lmist/masky-cli/internal/config"
	"github.com/lmist/masky-cli/internal/secureio"
	"github.com/lmist/masky-cli/internal/spec"
	"github.com/spf13/cobra"
)

type options struct {
	config, profile, base, format, selectField, saveJSON, errorFormat string
	timeout, waitTimeout, pollInterval                                time.Duration
	retries                                                           int
	allowInsecure, dryRun, yes, force, raw                            bool
}
type App struct {
	opts    options
	version string
	root    *cobra.Command
	secrets []string
}
type UsageError struct{ Message string }

func (e *UsageError) Error() string          { return e.Message }
func usage(format string, args ...any) error { return &UsageError{fmt.Sprintf(format, args...)} }

type JobError struct{ Message string }

func (e *JobError) Error() string { return e.Message }

func NewRoot(version string) *cobra.Command {
	a := &App{version: version}
	r := &cobra.Command{Use: "masky", Short: "The complete command-line client for Masky", Version: version, SilenceUsage: true, SilenceErrors: true,
		Long: "Generate media, manage avatars, run conversations, and integrate Masky from your terminal.\nJSON goes to stdout; errors and progress go to stderr. Run any command with --help.\nDocumentation: https://github.com/lmist/masky-cli"}
	a.root = r
	f := r.PersistentFlags()
	f.StringVar(&a.opts.config, "config", "", "Config file (or MASKY_CONFIG)")
	f.StringVar(&a.opts.profile, "profile", "", "Profile (or MASKY_PROFILE; default: active profile)")
	f.StringVar(&a.opts.base, "base-url", "", "API base URL (or MASKY_BASE_URL)")
	f.DurationVar(&a.opts.timeout, "timeout", 2*time.Minute, "Timeout for each HTTP request")
	f.IntVar(&a.opts.retries, "retries", 2, "Retries for transient GET failures only (0–10)")
	f.BoolVar(&a.opts.allowInsecure, "allow-insecure", false, "Allow remote HTTP URLs; HTTPS is the default")
	f.StringVar(&a.opts.format, "format", "json", "Output format: json or table")
	f.StringVar(&a.opts.selectField, "select", "", "Select a dot-separated JSON path (e.g. avatars.0.avatarId)")
	f.BoolVar(&a.opts.raw, "raw", false, "Print a selected scalar without JSON quoting")
	f.StringVar(&a.opts.saveJSON, "save-json", "", "Write output privately to a file instead of stdout")
	f.BoolVar(&a.opts.force, "force", false, "Allow replacing output files")
	f.BoolVar(&a.opts.dryRun, "dry-run", false, "Validate and print a redacted HTTP request without sending it")
	f.BoolVarP(&a.opts.yes, "yes", "y", false, "Confirm revocations, deletion, tribe fees, or gift confirmation")
	f.DurationVar(&a.opts.waitTimeout, "wait-timeout", 10*time.Minute, "Total time allowed for polling")
	f.DurationVar(&a.opts.pollInterval, "poll-interval", 2*time.Second, "Time between job polls")
	f.StringVar(&a.opts.errorFormat, "error-format", "text", "Error output: text or json")
	r.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if a.opts.format != "json" && a.opts.format != "table" {
			return usage("--format must be json or table")
		}
		if a.opts.errorFormat != "text" && a.opts.errorFormat != "json" {
			return usage("--error-format must be text or json")
		}
		if a.opts.raw && a.opts.selectField == "" {
			return usage("--raw requires --select")
		}
		if a.opts.timeout <= 0 || a.opts.waitTimeout <= 0 || a.opts.pollInterval <= 0 {
			return usage("timeouts and poll interval must be positive")
		}
		if a.opts.retries < 0 || a.opts.retries > 10 {
			return usage("--retries must be between 0 and 10")
		}
		return secureio.CheckOutput(a.opts.saveJSON, a.opts.force)
	}
	r.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage("%s", err) })
	endpoints, err := spec.Load()
	if err != nil {
		panic(err)
	}
	for _, e := range endpoints {
		a.addEndpoint(e)
	}
	a.addAuth()
	a.addAPI()
	a.addWaitCommands()
	a.addOAuth()
	a.addShare()
	r.AddCommand(&cobra.Command{Use: "version", Short: "Print the CLI version", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.output(cmd, map[string]any{"version": version})
	}})
	return r
}
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usage("%s accepts no positional arguments", cmd.CommandPath())
	}
	return nil
}
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usage("%s expects %d argument(s)", cmd.CommandPath(), n)
		}
		for _, arg := range args {
			if strings.TrimSpace(arg) == "" {
				return usage("arguments cannot be empty")
			}
		}
		return nil
	}
}
func (a *App) group(words []string) *cobra.Command {
	c := a.root
	for _, word := range words {
		var next *cobra.Command
		for _, child := range c.Commands() {
			if child.Name() == word {
				next = child
				break
			}
		}
		if next == nil {
			next = &cobra.Command{Use: word, Short: "Manage " + strings.ReplaceAll(word, "-", " ")}
			c.AddCommand(next)
		}
		c = next
	}
	return c
}
func (a *App) load() (string, *config.Config, string, error) {
	path, err := config.Path(a.opts.config)
	if err != nil {
		return "", nil, "", err
	}
	c, err := config.Load(path)
	if err != nil {
		return "", nil, "", err
	}
	name := config.ProfileName(a.opts.profile, c)
	return path, c, name, nil
}
func (a *App) client(auth string) (*api.Client, error) {
	_, c, name, err := a.load()
	if err != nil {
		return nil, err
	}
	base, token, _, err := config.Resolve(c, name, a.opts.base)
	if err != nil {
		return nil, usage("%s", err)
	}
	if auth == "session" {
		token = os.Getenv("MASKY_SESSION_TOKEN")
		if token == "" && !a.opts.dryRun {
			return nil, usage("this endpoint requires MASKY_SESSION_TOKEN (a first-party Firebase ID token)")
		}
		if strings.HasPrefix(token, "mky_") {
			return nil, usage("this endpoint requires a Firebase session; API keys and OAuth tokens are not accepted")
		}
	}
	if auth == "none" || auth == "nostr" {
		token = ""
	}
	if auth == "bearer" && token == "" && !a.opts.dryRun {
		return nil, &api.Error{Status: 401, Message: "no credential; set MASKY_API_KEY or run masky auth login --token-stdin"}
	}
	if token != "" && !config.ValidToken(token) {
		return nil, usage("credential contains whitespace")
	}
	a.secrets = append(a.secrets, token)
	return api.New(base, token, a.version, a.opts.timeout, a.opts.retries, a.opts.allowInsecure)
}
func (a *App) request(cmd *cobra.Command, c *api.Client, r api.Request) (any, error) {
	if a.opts.dryRun {
		u, err := c.URL(r.Path, r.Query)
		if err != nil {
			return nil, usage("%s", err)
		}
		headers := map[string]any{"Accept": "application/json"}
		if r.Body != nil {
			headers["Content-Type"] = "application/json"
		}
		if c.Token != "" || r.Authorization != "" {
			headers["Authorization"] = "[REDACTED]"
		}
		result := map[string]any{"method": r.Method, "url": u, "headers": headers, "body": r.Body}
		if r.Body != nil {
			b, _ := json.Marshal(r.Body)
			result["bodySha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
		}
		return api.Redact(result), nil
	}
	return c.Do(cmd.Context(), r)
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ue *UsageError
	var ae *api.Error
	var je *JobError
	switch {
	case errors.Is(err, context.Canceled):
		return 130
	case errors.Is(err, context.DeadlineExceeded):
		return 6
	case errors.As(err, &ue):
		return 2
	case errors.As(err, &je):
		return 7
	case errors.As(err, &ae):
		switch ae.Status {
		case 401, 403:
			return 3
		case 402:
			return 4
		case 429:
			return 5
		}
	}
	return 1
}
func Execute(ctx context.Context, args []string, in io.Reader, out, errout io.Writer, version string) int {
	r := NewRoot(version)
	r.SetArgs(args)
	r.SetIn(in)
	r.SetOut(out)
	r.SetErr(errout)
	_, err := r.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	code := ExitCode(err)
	message := api.RedactText(err.Error(), os.Getenv("MASKY_API_KEY"), os.Getenv("MASKY_SESSION_TOKEN"), os.Getenv("MASKY_CLIENT_SECRET"), os.Getenv("MASKY_CODE_VERIFIER"))
	if strings.HasPrefix(message, "unknown command ") {
		code = 2
	}
	format, _ := r.PersistentFlags().GetString("error-format")
	if format == "json" {
		_ = json.NewEncoder(errout).Encode(map[string]any{"error": message, "exitCode": code})
	} else {
		fmt.Fprintln(errout, "masky:", message)
	}
	return code
}
