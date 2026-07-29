// Package tunnel dials database connections through an SSH bastion host,
// for engines whose database port isn't directly reachable. It hands back
// a DialContext-compatible function that database/sql drivers (pgx,
// go-sql-driver/mysql) can register as their network dialer.
package tunnel

import (
	"context"
	"fmt"
	"net"
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
	// same format `ssh-keygen -lf -E sha256` prints). Left empty, the
	// tunnel accepts any host key — acceptable for a first connection in a
	// trusted network, but callers should surface this as a warning.
	HostKeyFingerprint string
}

const dialTimeout = 10 * time.Second

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
		HostKeyCallback: hostKeyCallback(cfg.HostKeyFingerprint),
		Timeout:         dialTimeout,
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("dialing SSH bastion %s: %w", host, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, host, clientCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH handshake with %s: %w", host, err)
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

func hostKeyCallback(fingerprint string) ssh.HostKeyCallback {
	if fingerprint == "" {
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != fingerprint {
			return fmt.Errorf("SSH host key mismatch for %s: got %s, want %s", hostname, got, fingerprint)
		}
		return nil
	}
}

// DialContext opens a connection to addr (the database's address) through
// the tunnel, matching the signature database/sql drivers expect for a
// custom dialer.
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := t.client.Dial(network, addr)
		ch <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

// Close shuts down the SSH connection and every tunneled connection.
func (t *Tunnel) Close() error {
	return t.client.Close()
}
