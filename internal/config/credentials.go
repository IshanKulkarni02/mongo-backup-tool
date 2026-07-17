package config

import (
	"net/url"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/secrets"
)

// credentialKey is the secrets-store key holding a connection's password.
func credentialKey(connName string) string {
	return "conn:" + connName
}

func sshPasswordKey(connName string) string {
	return "ssh-password:" + connName
}

func sshPrivateKeyKey(connName string) string {
	return "ssh-key:" + connName
}

// splitPassword separates the password from a connection URI. It reports
// ok only when the URI parses, contains a password, and the strip/inject
// round-trip reproduces the original exactly — anything else (exotic
// multi-host URIs, unparseable strings) is left alone so we never corrupt
// a working connection string.
func splitPassword(raw string) (stripped, password string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return "", "", false
	}
	pass, has := u.User.Password()
	if !has || pass == "" {
		return "", "", false
	}
	normalized := u.String()
	su := *u
	su.User = url.User(u.User.Username())
	stripped = su.String()
	if injectPassword(stripped, pass) != normalized {
		return "", "", false
	}
	return stripped, pass, true
}

// injectPassword re-adds a password to a credential-stripped URI. If the
// URI can't be parsed or has no username, it's returned unchanged.
func injectPassword(raw, password string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String()
}

// resolveCredentials re-injects keychain-held passwords into the loaded
// config's URIs, so everything downstream keeps working with complete
// connection strings. A missing or unreadable secret leaves the stripped
// URI in place (the connection will fail to authenticate, which surfaces
// the problem where the user can see it) rather than failing the load.
func resolveCredentials(cfg *Config) {
	for i := range cfg.Connections {
		c := &cfg.Connections[i]
		if c.CredentialRef != "" {
			if pass, err := secrets.Get(c.CredentialRef); err == nil {
				c.URI = injectPassword(c.URI, pass)
			}
		}
		if c.SSHPasswordRef != "" {
			if pass, err := secrets.Get(c.SSHPasswordRef); err == nil {
				c.SSHPassword = pass
			}
		}
		if c.SSHPrivateKeyRef != "" {
			if key, err := secrets.Get(c.SSHPrivateKeyRef); err == nil {
				c.SSHPrivateKey = key
			}
		}
	}
}

// stripCredentials returns a copy of the config safe to persist: when a
// system keyring is available, each connection's password is moved into it
// (verified by reading it back) and the stored URI keeps only the
// username. Connections whose password can't be safely split or stored
// keep their full URI on disk — the pre-keychain behavior, still protected
// by the file's 0600 mode.
func stripCredentials(cfg *Config) *Config {
	out := &Config{Connections: make([]Connection, len(cfg.Connections))}
	copy(out.Connections, cfg.Connections)
	if !secrets.Available() {
		return out
	}
	for i := range out.Connections {
		c := &out.Connections[i]
		if stripped, pass, ok := splitPassword(c.URI); ok {
			if key, stored := setSecretVerified(credentialKey(c.Name), pass); stored {
				c.URI = stripped
				c.CredentialRef = key
			}
		}
		if c.SSHPassword != "" {
			if key, stored := setSecretVerified(sshPasswordKey(c.Name), c.SSHPassword); stored {
				c.SSHPassword = ""
				c.SSHPasswordRef = key
			}
		}
		if c.SSHPrivateKey != "" {
			if key, stored := setSecretVerified(sshPrivateKeyKey(c.Name), c.SSHPrivateKey); stored {
				c.SSHPrivateKey = ""
				c.SSHPrivateKeyRef = key
			}
		}
	}
	return out
}

// setSecretVerified stores a secret and reads it back to confirm the
// write actually took (rather than trusting a keyring backend that
// reported success but silently dropped it), returning the key it was
// stored under and whether the round-trip succeeded. On failure the
// caller keeps the plaintext value in the persisted struct — the
// pre-keychain behavior, still protected by the config file's 0600 mode.
func setSecretVerified(key, value string) (string, bool) {
	if err := secrets.Set(key, value); err != nil {
		return "", false
	}
	back, err := secrets.Get(key)
	if err != nil || back != value {
		return "", false
	}
	return key, true
}

// DeleteCredential removes a connection's keychain entries, if any.
// Call when a connection is removed so no orphaned secrets accumulate.
func DeleteCredential(conn Connection) {
	if conn.CredentialRef != "" {
		_ = secrets.Delete(conn.CredentialRef)
	}
	if conn.SSHPasswordRef != "" {
		_ = secrets.Delete(conn.SSHPasswordRef)
	}
	if conn.SSHPrivateKeyRef != "" {
		_ = secrets.Delete(conn.SSHPrivateKeyRef)
	}
}

// MigrateCredentials moves any plaintext passwords in the stored config
// into the system keyring. It reports how many connections were migrated.
// A no-op (0, nil) when no keyring is available or nothing needs moving.
func MigrateCredentials() (int, error) {
	if !secrets.Available() {
		return 0, nil
	}
	cfg, err := Load()
	if err != nil {
		return 0, err
	}
	needs := 0
	for _, c := range cfg.Connections {
		if c.CredentialRef == "" {
			if _, _, ok := splitPassword(c.URI); ok {
				needs++
				continue
			}
		}
		if c.SSHPasswordRef == "" && c.SSHPassword != "" {
			needs++
			continue
		}
		if c.SSHPrivateKeyRef == "" && c.SSHPrivateKey != "" {
			needs++
		}
	}
	if needs == 0 {
		return 0, nil
	}
	if err := Save(cfg); err != nil {
		return 0, err
	}
	return needs, nil
}

// RedactURI masks a URI's password for safe display.
func RedactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPass := u.User.Password(); hasPass {
		u.User = url.UserPassword(u.User.Username(), "****")
	}
	// url.String() percent-encodes "*" in the userinfo component; undo that
	// so the mask reads as **** instead of %2A%2A%2A%2A.
	return strings.ReplaceAll(u.String(), "%2A", "*")
}
