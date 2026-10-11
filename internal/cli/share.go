package cli

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/spf13/cobra"
)

func parseQuery(pairs []string) (url.Values, error) {
	v := url.Values{}
	for _, pair := range pairs {
		k, s, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, usage("query parameters require key=value")
		}
		v.Add(k, s)
	}
	return v, nil
}
func validSlug(slug, kinds string) error {
	if !regexp.MustCompile(`^[` + kinds + `]-\d{2}-\d{2}-[a-z0-9]{4}$`).MatchString(slug) {
		return usage("invalid share slug; expected %s-YY-MM-xxxx", kinds)
	}
	return nil
}
func (a *App) addShare() {
	g := a.group([]string{"share"})
	for _, name := range []string{"url", "live", "resolve"} {
		name := name
		cmd := &cobra.Command{Use: name + " <slug>", Short: map[string]string{"url": "Build a share page URL", "live": "Build a live viewer URL or iframe", "resolve": "Resolve a public media share redirect without credentials"}[name], Args: exactArgs(1)}
		if name == "live" {
			cmd.Flags().String("viewer-token-file", "", "Read a private conversation viewer token from file (- for stdin)")
			cmd.Flags().String("since", "", "Only play turns since now, an ISO date, or epoch milliseconds")
			cmd.Flags().Bool("paused", false, "Start paused")
			cmd.Flags().Bool("iframe", false, "Include an embeddable iframe")
			cmd.Flags().Int("width", 640, "Iframe width")
			cmd.Flags().Int("height", 360, "Iframe height")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			kinds := "ctgv"
			if name == "live" {
				kinds = "ct"
			}
			if name == "resolve" {
				kinds = "gt"
			}
			if err := validSlug(args[0], kinds); err != nil {
				return err
			}
			c, err := a.client("none")
			if err != nil {
				return err
			}
			u, _ := url.Parse(c.BaseURL)
			u.Path = "/" + args[0]
			u.RawPath = ""
			if name == "live" {
				u.Path = "/live/" + args[0]
				q := url.Values{}
				if path, _ := cmd.Flags().GetString("viewer-token-file"); path != "" {
					b, err := readInput(cmd, path)
					if err != nil {
						return err
					}
					q.Set("token", strings.TrimSpace(string(b)))
				}
				if since, _ := cmd.Flags().GetString("since"); since != "" {
					q.Set("since", since)
				}
				if paused, _ := cmd.Flags().GetBool("paused"); paused {
					q.Set("autoplay", "0")
				}
				u.RawQuery = q.Encode()
				result := map[string]any{"url": u.String()}
				if iframe, _ := cmd.Flags().GetBool("iframe"); iframe {
					w, _ := cmd.Flags().GetInt("width")
					h, _ := cmd.Flags().GetInt("height")
					if w < 1 || h < 1 {
						return usage("iframe dimensions must be positive")
					}
					result["iframe"] = `<iframe src="` + html.EscapeString(u.String()) + `" allowfullscreen allow="fullscreen" style="border:0;width:` + strconv.Itoa(w) + `px;height:` + strconv.Itoa(h) + `px"></iframe>`
				}
				if a.opts.dryRun {
					return a.output(cmd, api.Redact(result))
				}
				return a.output(cmd, result)
			}
			if name == "url" || a.opts.dryRun {
				return a.output(cmd, map[string]any{"url": u.String()})
			}
			req, err := http.NewRequestWithContext(cmd.Context(), "GET", u.String(), nil)
			if err != nil {
				return err
			}
			resp, err := c.HTTP.Do(req)
			if err != nil {
				return fmt.Errorf("share resolution request failed")
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				location, err := resp.Location()
				if err != nil || !httpURL(location.String()) {
					return usage("server returned an invalid share redirect")
				}
				return a.output(cmd, map[string]any{"url": location.String(), "status": resp.StatusCode})
			}
			if resp.StatusCode == 200 {
				return a.output(cmd, map[string]any{"url": u.String(), "status": 200, "note": "This share is a viewer page, not a media redirect."})
			}
			return fmt.Errorf("share resolution: HTTP %d", resp.StatusCode)
		}
		g.AddCommand(cmd)
	}
}
