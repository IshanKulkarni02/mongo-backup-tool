package service

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// ConnectionInput describes a connection to save. SSHHost being non-empty
// enables an SSH tunnel; SSHPrivateKey (a PEM-encoded key) takes precedence
// over SSHPassword if both are set.
type ConnectionInput struct {
	Name             string
	URI              string
	Engine           string // empty means mongodb
	Environment      string
	ReadOnly         bool
	SSHHost          string
	SSHUser          string
	SSHPassword      string
	SSHPrivateKey    string
	TenantSessionVar string
	// AgentAccess is off|read|write. Empty keeps an existing connection's
	// current setting (and means off for a new one), so re-saving a profile
	// never silently changes what agents may do.
	AgentAccess string
}

// AddConnection validates and saves a connection, replacing one with the
// same name. Callers holding a session cache should Invalidate the name.
func AddConnection(in ConnectionInput) error {
	if in.Name == "" || in.URI == "" {
		return fmt.Errorf("both a name and a URI are required")
	}
	engineID := in.Engine
	if engineID == "" {
		engineID = "mongodb"
	}
	if _, err := engine.Lookup(engineID); err != nil {
		return err
	}
	switch in.Environment {
	case "", "dev", "staging", "prod":
	default:
		return fmt.Errorf("invalid environment %q (use dev, staging, or prod)", in.Environment)
	}
	if in.SSHHost != "" && in.SSHPassword == "" && in.SSHPrivateKey == "" {
		return fmt.Errorf("an SSH tunnel needs a password or private key")
	}
	if in.TenantSessionVar != "" && !engine.ValidSessionVarName(in.TenantSessionVar) {
		return fmt.Errorf("invalid tenant session variable name %q", in.TenantSessionVar)
	}
	switch in.AgentAccess {
	case "", "off", "read", "write":
	default:
		return fmt.Errorf("invalid agent access %q (use off, read, or write)", in.AgentAccess)
	}
	return config.Update(func(cfg *config.Config) error {
		// Preserve state that a re-save of the profile must not reset: the
		// current tenant, and what agents may do.
		tenantValue, agentAccess := "", in.AgentAccess
		if existing, ok := cfg.Find(in.Name); ok {
			tenantValue = existing.TenantValue
			if agentAccess == "" {
				agentAccess = existing.AgentAccess
			}
		}
		if agentAccess == "off" {
			agentAccess = ""
		}
		cfg.Upsert(config.Connection{
			Name:             in.Name,
			URI:              in.URI,
			Engine:           engineID,
			Environment:      in.Environment,
			ReadOnly:         in.ReadOnly,
			AgentAccess:      agentAccess,
			SSHHost:          in.SSHHost,
			SSHUser:          in.SSHUser,
			SSHPassword:      in.SSHPassword,
			SSHPrivateKey:    in.SSHPrivateKey,
			TenantSessionVar: in.TenantSessionVar,
			TenantValue:      tenantValue,
			CreatedAt:        time.Now().Format(time.RFC3339),
		})
		return nil
	})
}

// RemoveConnection deletes a saved connection and its keychain entries.
func RemoveConnection(name string) error {
	return config.Update(func(cfg *config.Config) error {
		if conn, ok := cfg.Find(name); ok {
			config.DeleteCredential(*conn)
		}
		if !cfg.Remove(name) {
			return fmt.Errorf("no connection named %q", name)
		}
		return nil
	})
}

var dsnUserRe = regexp.MustCompile(`^([^:@/]+)(?::[^@]*)?@((?:tcp|unix)\()`)

// SetConnectionPassword sets the password of a saved connection's URI without
// the caller ever needing to see the rest of it. It understands URL-style
// URIs (postgres://user@host/db, mongodb://user@host) and MySQL's DSN form
// (user@tcp(host:3306)/db). The connection must already name a user.
func SetConnectionPassword(name, password string) error {
	return config.Update(func(cfg *config.Config) error {
		conn, ok := cfg.Find(name)
		if !ok {
			return fmt.Errorf("no connection named %q", name)
		}
		if strings.Contains(conn.URI, "://") {
			u, err := url.Parse(conn.URI)
			if err != nil {
				return fmt.Errorf("the saved URI can't be parsed")
			}
			if u.User == nil || u.User.Username() == "" {
				return fmt.Errorf("the URI has no username; add one (user@host) before setting a password")
			}
			u.User = url.UserPassword(u.User.Username(), password)
			conn.URI = u.String()
			return nil
		}
		if dsnUserRe.MatchString(conn.URI) {
			conn.URI = dsnUserRe.ReplaceAllString(conn.URI, "${1}:"+strings.ReplaceAll(password, "$", "$$")+"@${2}")
			return nil
		}
		return fmt.Errorf("this connection's URI has no user to attach a password to")
	})
}
