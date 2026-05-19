package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const envCadenceTokenFile = "CADENCE_TOKEN_FILE"
const flagAuthProfile = "profile"

type authFile struct {
	Entries []*authEntry `json:"entries"`
}

type authEntry struct {
	Profile      string    `json:"profile"`
	IssuerURL    string    `json:"issuer_url"`
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// 30s cushion to avoid racing the server clock.
func (e *authEntry) expired() bool {
	return time.Now().Add(30 * time.Second).After(e.ExpiresAt)
}

func (f *authFile) lookupByProfile(profile string) *authEntry {
	for _, e := range f.Entries {
		if e.Profile == profile {
			return e
		}
	}
	return nil
}

// put inserts or replaces an entry, keyed only by profile name. Multiple
// profiles for the same (issuer, client_id) are allowed (e.g. alice and bob
// both holding tokens for the same cluster).
func (f *authFile) put(entry *authEntry) {
	out := f.Entries[:0]
	for _, e := range f.Entries {
		if e.Profile == entry.Profile {
			continue
		}
		out = append(out, e)
	}
	f.Entries = append(out, entry)
}

func (f *authFile) removeByProfile(profile string) bool {
	for i, e := range f.Entries {
		if e.Profile == profile {
			f.Entries = append(f.Entries[:i], f.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// autoProfile derives the deterministic default label for a (issuer, client_id)
// pair when the user hasn't picked one via --profile. Stays human-readable
// while still being unique across dev/staging/prod that share an OIDC host.
func autoProfile(issuer, clientID string) string {
	host := issuer
	if u, err := url.Parse(issuer); err == nil && u.Host != "" {
		host = u.Host
		if u.Path != "" && u.Path != "/" {
			host += u.Path
		}
	}
	return fmt.Sprintf("%s@%s", clientID, host)
}

func authFilePath() (string, error) {
	if p := os.Getenv(envCadenceTokenFile); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cadence", "auth.json"), nil
}

func loadAuthFile() (*authFile, error) {
	path, err := authFilePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &authFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f authFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &f, nil
}

func saveAuthFile(f *authFile) error {
	path, err := authFilePath()
	if err != nil {
		return err
	}
	if len(f.Entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func lookupEntryByProfile(profile string) *authEntry {
	f, err := loadAuthFile()
	if err != nil {
		return nil
	}
	return f.lookupByProfile(profile)
}

func saveEntry(entry *authEntry) error {
	f, err := loadAuthFile()
	if err != nil {
		return err
	}
	f.put(entry)
	return saveAuthFile(f)
}
