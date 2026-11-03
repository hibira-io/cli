// Package config stores named credential profiles in the user's config
// directory (see os.UserConfigDir) with 0600 permissions.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// DefaultHost is the shared Hibira endpoint.
const DefaultHost = "ws.hibira.io"

// Profile is one set of app credentials.
type Profile struct {
	AppID  string `json:"app_id,omitempty"`
	Key    string `json:"key,omitempty"`
	Secret string `json:"secret,omitempty"`
	Host   string `json:"host,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// File is the whole config file.
type File struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles"`
}

// Path returns the config file location. HIBIRA_CONFIG overrides it.
func Path() (string, error) {
	if p := os.Getenv("HIBIRA_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hibira", "config.json"), nil
}

// Load reads the file; a missing file is an empty config.
func Load(path string) (*File, error) {
	f := &File{Profiles: map[string]Profile{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Profiles == nil {
		f.Profiles = map[string]Profile{}
	}
	return f, nil
}

// Save writes the file atomically with owner-only permissions.
func Save(path string, f *File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Names returns the profile names in order.
func (f *File) Names() []string {
	out := make([]string, 0, len(f.Profiles))
	for n := range f.Profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Active returns the profile name to use when none is given.
func (f *File) Active() string {
	if f.Default != "" {
		return f.Default
	}
	return "default"
}
