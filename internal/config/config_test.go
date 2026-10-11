package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigPrivateAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Profiles["default"] = Profile{Token: "fixture-token", BaseURL: DefaultBaseURL}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("unsafe permissions")
	}
	got, err := Load(path)
	if err != nil || got.Profiles["default"].Token != "fixture-token" {
		t.Fatalf("load: %+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0644)
		if _, err = Load(path); err == nil {
			t.Fatal("accepted world-readable credential file")
		}
	}
}
func TestPrecedenceAndOriginBinding(t *testing.T) {
	t.Setenv("MASKY_API_KEY", "")
	t.Setenv("MASKY_BASE_URL", "")
	c := &Config{Current: "default", Profiles: map[string]Profile{"default": {Token: "stored", BaseURL: DefaultBaseURL}}}
	base, token, source, err := Resolve(c, "default", "")
	if err != nil || base != DefaultBaseURL || token != "stored" || source != "profile" {
		t.Fatal("profile not resolved")
	}
	_, token, _, err = Resolve(c, "default", "https://other.test/api")
	if err != nil || token != "" {
		t.Fatal("stored credential escaped its base URL")
	}
	t.Setenv("MASKY_API_KEY", "env-token")
	_, token, source, err = Resolve(c, "default", "")
	if err != nil || token != "env-token" || source != "MASKY_API_KEY" {
		t.Fatal("environment precedence failed")
	}
	t.Setenv("MASKY_BASE_URL", "https://env.test/api")
	base, _, _, _ = Resolve(c, "default", "https://flag.test/api")
	if base != "https://flag.test/api" {
		t.Fatal("flag precedence failed")
	}
	if _, _, _, err = Resolve(c, "typo", ""); err == nil {
		t.Fatal("unknown profile silently fell back")
	}
}
func TestRejectSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "config")
	_ = os.WriteFile(target, []byte(`{"version":1}`), 0600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("followed config symlink")
	}
	if err := Save(link, &Config{Version: 1}); err == nil {
		t.Fatal("overwrote config symlink")
	}
}
func TestMalformedAndUnsupported(t *testing.T) {
	for _, body := range []string{"{", `{"version":2}`, `{"version":1} {}`} {
		path := filepath.Join(t.TempDir(), "config")
		_ = os.WriteFile(path, []byte(body), 0600)
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
