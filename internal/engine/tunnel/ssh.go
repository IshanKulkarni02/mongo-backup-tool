// Package tunnel dials database connections through an SSH bastion host,
// for engines whose database port isn't directly reachable. It hands back
// a DialContext-compatible function that database/sql drivers (pgx,
// go-sql-driver/mysql) can register as their network dialer.
package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config describes how to reach and authenticate to the SSH bastion.
// Exactly one of Password or PrivateKeyPEM should be set; PrivateKeyPEM
// wins if both are.
type Config struct {
	// Host is the bastion's address, host:port (port defaults to 22 if
	// omitted).
	Host          string
	User          string
	Password      string
	PrivateKeyPEM string
	// PrivateKeyPassphrase decrypts PrivateKeyPEM if it's an encrypted key.
	PrivateKeyPassphrase string
	// HostKeyFingerprint pins the expected host key (base64 SHA256, the
	// same format `ssh-keygen -lf -E sha256` prints). Takes precedence over
	// KnownHostsPath when set.
	HostKeyFingerprint string
	// KnownHostsPath is where trust-on-first-use host key fingerprints are
	// recorded (see tofuHostKeyCallback), used when HostKeyFingerprint
	// isn't set. Left empty (with HostKeyFingerprint also empty), the
	// tunnel falls back to accepting any host key with no verification at
	// all — every real caller should set this; it's only ever empty in
	// tests that don't care about host-key behavior.
	KnownHostsPath string
}

// dialTimeout bounds both the TCP dial and the SSH handshake in Open. A
// var (not a const) so tests can shrink it instead of waiting out the
// real default when exercising a bastion that stalls on purpose.
var dialTimeout = 10 * time.Second

// Tunnel holds one live SSH connection. Dial opens a new logical
// connection to a target address through it; Close tears down the
// underlying SSH connection and every tunneled connection with it.
type Tunnel struct {
	client *ssh.Client
}

// Open establishes the SSH connection. The returned Tunnel must be closed
// by the caller once no longer needed.
func Open(ctx context.Context, cfg Config) (*Tunnel, error) {
	auth, err := authMethod(cfg)
	if err != nil {
		return nil, err
	}
	host := cfg.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "22")
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: hostKeyCallback(cfg, host),
		Timeout:         dialTimeout,
	}

	// clientCfg.Timeout above only takes effect inside the ssh package's
	// own Dial() convenience wrapper — this code calls NewClientConn
	// directly (below), so that field is dead configuration and has zero
	// effect on either the TCP dial or the handshake that follows. Every
	// real call site in this codebase passes context.Background() (no
	// deadline of its own), so without an explicit bound here, a bastion
	// that accepts the TCP connection but stalls during key
	// exchange/auth hangs the connection attempt forever with no way to
	// cancel it. deadlineCtx guarantees an upper bound regardless of
	// what the caller passes: context.WithTimeout takes whichever of the
	// two deadlines is sooner, so a caller-supplied shorter deadline
	// still wins.
	deadlineCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(deadlineCtx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("dialing SSH bastion %s: %w", host, err)
	}

	// NewClientConn's handshake is synchronous with no context support,
	// so a deadline on the raw connection is the only way to bound it.
	// Set one before the handshake, then clear it once NewClientConn
	// returns — otherwise the deadline would also apply to the tunnel's
	// ongoing data transfer after setup completes.
	deadline, _ := deadlineCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting SSH handshake deadline for %s: %w", host, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, host, clientCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH handshake with %s: %w", host, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("clearing SSH handshake deadline for %s: %w", host, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	return &Tunnel{client: client}, nil
}

func authMethod(cfg Config) (ssh.AuthMethod, error) {
	if cfg.PrivateKeyPEM != "" {
		var signer ssh.Signer
		var err error
		if cfg.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKeyPEM), []byte(cfg.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKeyPEM))
		}
		if err != nil {
			return nil, fmt.Errorf("parsing SSH private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	}
	if cfg.Password != "" {
		return ssh.Password(cfg.Password), nil
	}
	return nil, fmt.Errorf("SSH tunnel requires a password or private key")
}

// hostKeyCallback picks the strongest verification available: an exact
// pinned fingerprint if the caller configured one, otherwise trust-on-
// first-use (TOFU) against a local known-hosts file if the caller
// configured a path for one, otherwise — only when neither is set —
// falling back to accepting any host key with zero verification. Every
// real call site in this codebase sets KnownHostsPath, so the last case
// only applies to tests that don't care about host-key behavior.
func hostKeyCallback(cfg Config, host string) ssh.HostKeyCallback {
	if cfg.HostKeyFingerprint != "" {
		return pinnedHostKeyCallback(cfg.HostKeyFingerprint)
	}
	if cfg.KnownHostsPath != "" {
		return tofuHostKeyCallback(cfg.KnownHostsPath, host)
	}
	return ssh.InsecureIgnoreHostKey()
}

func pinnedHostKeyCallback(fingerprint string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != fingerprint {
			return fmt.Errorf("SSH host key mismatch for %s: got %s, want %s", hostname, got, fingerprint)
		}
		return nil
	}
}

// tofuHostKeyCallback implements trust-on-first-use host key pinning: the
// first time a host is seen, its key fingerprint is recorded to
// knownHostsPath and the connection is allowed — the same trust moment
// every SSH client and browser TLS-TOFU flow accepts, and the practical
// middle ground between "warn but always allow" (no real protection) and
// "refuse to connect until a fingerprint is pre-configured" (unusable
// without UI/CLI plumbing for every connection). Every subsequent
// connection to that host must present the exact same key or the
// connection is refused outright — this is what actually closes the MITM
// gap InsecureIgnoreHostKey left wide open: an attacker impersonating a
// host the user has already connected to before is now caught.
func tofuHostKeyCallback(knownHostsPath, host string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		hosts, err := loadKnownHosts(knownHostsPath)
		if err != nil {
			return fmt.Errorf("reading SSH known-hosts file %s: %w", knownHostsPath, err)
		}
		if want, ok := hosts[host]; ok {
			if got != want {
				return fmt.Errorf("SSH host key for %s has changed (was %s, now %s) — this could mean someone is intercepting your connection, or the server was legitimately reconfigured/reinstalled; if you're certain it's the latter, remove %s's entry from %s and reconnect", host, want, got, host, knownHostsPath)
			}
			return nil
		}
		hosts[host] = got
		if err := saveKnownHosts(knownHostsPath, hosts); err != nil {
			return fmt.Errorf("recording SSH host key for %s: %w", host, err)
		}
		return nil
	}
}

func loadKnownHosts(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var hosts map[string]string
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

func saveKnownHosts(path string, hosts map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// DialContext opens a connection to addr (the database's address) through
// the tunnel, matching the signature database/sql drivers expect for a
// custom dialer.
//
// t.client.Dial has no context/cancellation support, so it runs on its own
// goroutine while this function races it against ctx.Done(). If ctx wins,
// the dial may still be in flight and can still succeed afterward — that
// net.Conn (a live SSH channel) would have no owner unless something
// closes it. abandoned, guarded by mu, is the single source of truth both
// sides consult: whichever of "the dial finished" and "ctx fired" reaches
// the mutex first decides the outcome, so there's no window where the
// connection is both handed off to nobody and left open. (A buffered
// channel plus a non-blocking send/select on it — the more obvious-looking
// fix — doesn't actually work here: a size-1 buffered channel with a
// single sender never blocks, so a `select` with `default` around the send
// would never take the default branch and never detect an abandoned
// caller.)
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	var mu sync.Mutex
	abandoned := false

	go func() {
		c, err := t.client.Dial(network, addr)
		mu.Lock()
		defer mu.Unlock()
		if abandoned {
			if err == nil && c != nil {
				c.Close()
			}
			return
		}
		ch <- result{c, err}
	}()

	select {
	case <-ctx.Done():
		mu.Lock()
		abandoned = true
		mu.Unlock()
		// The dial may have already completed and sent into ch's buffer
		// in the brief window before this branch acquired mu (Go's select
		// picks pseudo-randomly when both cases are simultaneously ready,
		// so ctx.Done() can still be chosen even after a successful send).
		// Because the goroutine above sends to ch only while still holding
		// mu, that send is guaranteed to have already completed by the
		// time this Lock/Unlock returns if it was going to happen at all
		// — so a non-blocking drain here can never miss it.
		select {
		case r := <-ch:
			if r.err == nil && r.conn != nil {
				r.conn.Close()
			}
		default:
		}
		return nil, ctx.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

// Close shuts down the SSH connection and every tunneled connection.
func (t *Tunnel) Close() error {
	return t.client.Close()
}
