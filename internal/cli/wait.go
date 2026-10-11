package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lmist/masky-cli/internal/api"
	"github.com/spf13/cobra"
)

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func jobObject(v any) map[string]any {
	m := object(v)
	for _, key := range []string{"generation", "turn"} {
		if nested := object(m[key]); nested != nil {
			return nested
		}
	}
	return m
}
func mediaURL(v any, output string) string {
	m := jobObject(v)
	if output == "video" {
		return str(m, "videoUrl")
	}
	if output == "audio" {
		return str(m, "audioUrl")
	}
	for _, k := range []string{"videoUrl", "audioUrl", "imageUrl"} {
		if s := str(m, k); s != "" {
			return s
		}
	}
	return ""
}
func jobReady(v any, kind, output string) (bool, error) {
	m := jobObject(v)
	status := strings.ToLower(str(m, "status"))
	if status == "error" || status == "failed" {
		message := str(m, "errorMessage")
		if message == "" {
			message = str(m, "error")
		}
		if message == "" {
			message = "generation failed"
		}
		return false, &JobError{api.RedactText(message)}
	}
	if kind == "donation" {
		switch status {
		case "confirmed", "cancelled", "expired":
			return true, nil
		}
		return false, nil
	}
	if kind == "video" {
		return status == "ready" && str(m, "videoUrl") != "", nil
	}
	if output == "" {
		output = str(m, "output")
		if output == "" {
			output = "audio"
		}
	}
	if output == "text" {
		return status == "ready", nil
	}
	// A voice-less conversation may explicitly report a text fallback.
	if kind == "turn" && str(m, "output") == "text" && status == "ready" {
		return true, nil
	}
	if kind == "turn" && m["output"] == nil && status == "ready" && str(m, "avatarText") != "" {
		return true, nil
	}
	if output == "video" && str(m, "videoError") != "" {
		return false, &JobError{api.RedactText(str(m, "videoError"))}
	}
	return mediaURL(m, output) != "", nil
}
func (a *App) poll(cmd *cobra.Command, c *api.Client, path, kind, output, turnID string, initial any) (any, error) {
	ctx, cancel := context.WithTimeout(cmd.Context(), a.opts.waitTimeout)
	defer cancel()
	current := initial
	lastStatus := ""
	for {
		selected := current
		if turnID != "" {
			selected = findTurn(current, turnID)
		}
		if selected != nil {
			ready, err := jobReady(selected, kind, output)
			if err != nil {
				return nil, err
			}
			if ready {
				return selected, nil
			}
			status := str(jobObject(selected), "status")
			if status != "" && status != lastStatus {
				fmt.Fprintln(cmd.ErrOrStderr(), "Status:", status)
				lastStatus = status
			}
		}
		timer := time.NewTimer(a.opts.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("waiting for %s: %w (resume with the corresponding wait command)", kind, ctx.Err())
		case <-timer.C:
		}
		var err error
		current, err = c.Do(ctx, api.Request{Method: "GET", Path: path})
		if err != nil {
			return nil, err
		}
	}
}
func findTurn(v any, id string) any {
	m := object(v)
	if str(m, "id") == id {
		return m
	}
	if t := object(m["turn"]); str(t, "id") == id {
		return t
	}
	if list, ok := m["turns"].([]any); ok {
		for _, item := range list {
			if str(object(item), "id") == id {
				return item
			}
		}
	}
	if conv := object(m["conversation"]); conv != nil {
		return findTurn(conv, id)
	}
	return nil
}
func (a *App) waitResult(cmd *cobra.Command, c *api.Client, kind string, result any, body map[string]any, args []string, slug string) (any, error) {
	output := str(body, "output")
	if output == "" {
		output = str(jobObject(result), "output")
	}
	if kind != "turn" {
		if ready, err := jobReady(result, kind, output); ready || err != nil {
			return result, err
		}
	}
	m := jobObject(result)
	id := str(m, "generationId")
	if id == "" {
		id = str(object(result), "generationId")
	}
	if id == "" {
		id = str(m, "id")
	}
	var path, turnID string
	switch kind {
	case "video", "speak":
		if id == "" && cmd.Name() == "get" && len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			return nil, &JobError{"server did not return a generation ID; cannot poll"}
		}
		path = "/videos/" + url.PathEscape(id)
		if kind == "speak" {
			path = "/avatars/speak/" + url.PathEscape(id)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Waiting for", kind, id)
	case "turn":
		turnID = str(m, "id")
		if turnID == "" {
			return nil, &JobError{"server did not return a turn ID; cannot poll"}
		}
		path = "/conversations/by-slug/" + url.PathEscape(slug)
		fmt.Fprintln(cmd.ErrOrStderr(), "Waiting for turn", turnID, "in", slug)
	case "donation":
		id = str(m, "intentId")
		if id == "" && len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			return nil, &JobError{"server did not return an intent ID; cannot poll"}
		}
		if confirm := str(m, "confirmUrl"); confirm != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Confirm the gift in your browser:", confirm)
		}
		path = "/donations/intents/" + url.PathEscape(id)
	}
	return a.poll(cmd, c, path, kind, output, turnID, result)
}
func (a *App) addWaitCommands() {
	for _, kind := range []string{"videos", "speak", "donations", "conversations"} {
		kind := kind
		use := "wait <id>"
		n := 1
		if kind == "conversations" {
			use = "wait <slug> <turn-id>"
			n = 2
		}
		cmd := &cobra.Command{Use: use, Short: "Wait for an existing job without creating another", Args: exactArgs(n)}
		if kind != "donations" {
			cmd.Flags().String("output", "", "Media to wait for: text, audio, or video (default: job output)")
			cmd.Flags().String("download", "", "Download completed media to this path")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			if output != "" && output != "audio" && output != "video" && output != "text" {
				return usage("--output must be text, audio, or video")
			}
			for _, arg := range args {
				if strings.ContainsAny(arg, "/\\") || arg == "." || arg == ".." {
					return usage("invalid identifier")
				}
			}
			c, err := a.client("bearer")
			if err != nil {
				return err
			}
			jobKind, path, turnID := kind, "", ""
			switch kind {
			case "videos":
				jobKind = "video"
				path = "/videos/" + url.PathEscape(args[0])
			case "speak":
				path = "/avatars/speak/" + url.PathEscape(args[0])
			case "donations":
				jobKind = "donation"
				path = "/donations/intents/" + url.PathEscape(args[0])
			case "conversations":
				jobKind = "turn"
				if err := validSlug(args[0], "c"); err != nil {
					return err
				}
				path = "/conversations/by-slug/" + args[0]
				turnID = args[1]
			}
			initial, err := a.request(cmd, c, api.Request{Method: "GET", Path: path})
			if err != nil {
				return err
			}
			if a.opts.dryRun {
				return a.output(cmd, initial)
			}
			result, err := a.poll(cmd, c, path, jobKind, output, turnID, initial)
			if err != nil {
				return err
			}
			if download, _ := cmd.Flags().GetString("download"); download != "" {
				media := mediaURL(result, output)
				if media == "" {
					return &JobError{"completed job has no requested media"}
				}
				if err := c.Download(cmd.Context(), media, download, a.opts.force); err != nil {
					return err
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "Saved media to", download)
			}
			return a.output(cmd, result)
		}
		a.group([]string{kind}).AddCommand(cmd)
	}
}
