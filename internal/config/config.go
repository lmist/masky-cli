package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lmist/masky-cli/internal/secureio"
)

const DefaultBaseURL = "https://masky.ai/api"

type Profile struct {
	Token   string `json:"token,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}
type Config struct {
	Version  int                `json:"version"`
	Current  string             `json:"current"`
	Profiles map[string]Profile `json:"profiles"`
}

func Path(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v := os.Getenv("MASKY_CONFIG"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "masky", "config.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "masky", "config.json"), nil
}
func Load(path string) (*Config, error) {
	c := &Config{Version: 1, Current: "default", Profiles: map[string]Profile{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config must be a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("config is readable by other users; run chmod 600 %s", path)
	}
	b, err := secureio.ReadFile(path, 1<<20)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, c) != nil {
		return nil, fmt.Errorf("invalid config JSON in %s", path)
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("unsupported config version %d", c.Version)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	if c.Current == "" {
		c.Current = "default"
	}
	return c, nil
}
func Save(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("config must be a regular file: %s", path)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return secureio.Write(path, true, func(w io.Writer) error { _, err := io.Copy(w, bytes.NewReader(b)); return err })
}
func ProfileName(flag string, c *Config) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("MASKY_PROFILE"); v != "" {
		return v
	}
	return c.Current
}
func ValidToken(token string) bool { return token != "" && !strings.ContainsAny(token, " \t\n\r") }

// Resolve binds stored credentials to their saved API origin and base path.
// Environment credentials are explicit per-process overrides.
func Resolve(c *Config, name, baseFlag string) (base, token, source string, err error) {
	p, exists := c.Profiles[name]
	if !exists && name != "default" {
		return "", "", "", fmt.Errorf("unknown profile %q", name)
	}
	base = p.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	savedBase := strings.TrimRight(base, "/")
	if v := os.Getenv("MASKY_BASE_URL"); v != "" {
		base = v
	}
	if baseFlag != "" {
		base = baseFlag
	}
	base = strings.TrimRight(base, "/")
	if token = os.Getenv("MASKY_API_KEY"); token != "" {
		source = "MASKY_API_KEY"
	} else if base == savedBase {
		token = p.Token
		if token != "" {
			source = "profile"
		}
	}
	if token != "" && !ValidToken(token) {
		return "", "", "", fmt.Errorf("credential contains whitespace")
	}
	return base, token, source, nil
}
