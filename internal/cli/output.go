package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/lmist/masky-cli/internal/secureio"
	"github.com/spf13/cobra"
)

func (a *App) output(cmd *cobra.Command, value any) error {
	var err error
	if a.opts.selectField != "" {
		value, err = selectValue(value, a.opts.selectField)
		if err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	if a.opts.raw {
		switch v := value.(type) {
		case string:
			fmt.Fprintln(&buf, v)
		case nil:
			fmt.Fprintln(&buf, "null")
		case bool, float64, json.Number, int, int64:
			fmt.Fprintln(&buf, v)
		default:
			return usage("--raw requires a scalar selection")
		}
	} else if a.opts.format == "table" {
		if err = writeTable(&buf, value); err != nil {
			return err
		}
	} else {
		e := json.NewEncoder(&buf)
		e.SetIndent("", "  ")
		e.SetEscapeHTML(false)
		if err = e.Encode(value); err != nil {
			return err
		}
	}
	if a.opts.saveJSON != "" {
		return secureio.Write(a.opts.saveJSON, a.opts.force, func(w io.Writer) error { _, err := io.Copy(w, &buf); return err })
	}
	_, err = io.Copy(cmd.OutOrStdout(), &buf)
	return err
}
func selectValue(value any, path string) (any, error) {
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return value, nil
	}
	for _, part := range strings.Split(path, ".") {
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				return nil, usage("selection path does not exist: %s", path)
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				return nil, usage("invalid array index in selection: %s", path)
			}
			value = v[i]
		default:
			return nil, usage("cannot traverse selection: %s", path)
		}
	}
	return value, nil
}
func cell(value any) string {
	var s string
	if v, ok := value.(string); ok {
		s = v
	} else if value != nil {
		b, _ := json.Marshal(value)
		s = string(b)
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func writeTable(w io.Writer, value any) error {
	if m, ok := value.(map[string]any); ok {
		for _, key := range []string{"avatars", "voices", "apps", "grants", "keys", "images", "gifts", "profiles"} {
			if list, ok := m[key].([]any); ok {
				value = list
				break
			}
		}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if list, ok := value.([]any); ok {
		columns := map[string]bool{}
		for _, row := range list {
			if m, ok := row.(map[string]any); ok {
				for k := range m {
					columns[k] = true
				}
			} else {
				return usage("table output requires objects; use --format json")
			}
		}
		keys := make([]string, 0, len(columns))
		for k := range columns {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(list) == 0 {
			fmt.Fprintln(tw, "(empty)")
		} else {
			fmt.Fprintln(tw, strings.Join(keys, "\t"))
			for _, row := range list {
				m := row.(map[string]any)
				cells := make([]string, len(keys))
				for i, k := range keys {
					cells[i] = cell(m[k])
				}
				fmt.Fprintln(tw, strings.Join(cells, "\t"))
			}
		}
	} else if m, ok := value.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(tw, "%s\t%s\n", cell(k), cell(m[k]))
		}
	} else {
		fmt.Fprintln(tw, cell(value))
	}
	return tw.Flush()
}
