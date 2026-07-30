package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startEchoServer runs a TCP listener that echoes back whatever it reads,
// standing in for "the database" on the far side of the tunnel.
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting echo listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// newTestServerConfig builds an in-process SSH server config accepting the
// given password and/or public key — either may be left zero-valued to
// disable that auth method — so tests can exercise password auth and
// public-key auth without a real bastion host.
func newTestServerConfig(t *testing.T, user, password string, authorizedKey ssh.PublicKey) *ssh.ServerConfig {
	t.Helper()
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &ssh.ServerConfig{}
	if password != "" {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		}
	}
	if authorizedKey != nil {
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == user && bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		}
	}
	cfg.AddHostKey(signer)
	return cfg
}

// startListening runs the accept loop for an in-process SSH server built
// from cfg and returns its address.
func startListening(t *testing.T, cfg *ssh.ServerConfig) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting SSH listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSSHConn(conn, cfg)
		}
	}()
	return ln.Addr().String()
}

// startSSHServer runs a minimal in-process SSH server that accepts a fixed
// password and forwards direct-tcpip channels (what ssh.Client.Dial opens)
// to whatever address the client asked for — a stand-in for a real
// bastion host, so the tunnel dialer can be tested without one.
func startSSHServer(t *testing.T) (addr, user, password string) {
	t.Helper()
	user, password = "tunneluser", "s3cret"
	cfg := newTestServerConfig(t, user, password, nil)
	return startListening(t, cfg), user, password
}

// startSSHServerWithKey is startSSHServer's public-key-auth counterpart: it
// accepts only connections presenting authorizedKey, no password.
func startSSHServerWithKey(t *testing.T, authorizedKey ssh.PublicKey) (addr, user string) {
	t.Helper()
	user = "tunneluser"
	cfg := newTestServerConfig(t, user, "", authorizedKey)
	return startListening(t, cfg), user
}

// generateTestKeyPair returns an unencrypted PEM-encoded RSA private key
// and its corresponding public key, for private-key-auth tests.
func generateTestKeyPair(t *testing.T) (privatePEM []byte, public ssh.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating client key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatalf("marshaling private key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return pem.EncodeToMemory(block), signer.PublicKey()
}

func handleSSHConn(conn net.Conn, cfg *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "direct-tcpip" {
			newCh.Reject(ssh.UnknownChannelType, "only direct-tcpip is supported")
			continue
		}
		var payload struct {
			DestAddr string
			DestPort uint32
			OrigAddr string
			OrigPort uint32
		}
		if err := ssh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
			newCh.Reject(ssh.ConnectionFailed, "bad request")
			continue
		}
		target := net.JoinHostPort(payload.DestAddr, itoa(payload.DestPort))
		targetConn, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			newCh.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			targetConn.Close()
			continue
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			defer ch.Close()
			defer targetConn.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(targetConn, ch); done <- struct{}{} }()
			go func() { io.Copy(ch, targetConn); done <- struct{}{} }()
			<-done
		}()
	}
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestTunnelProxiesConnectionToTarget(t *testing.T) {
	echoAddr := startEchoServer(t)
	bastionAddr, user, password := startSSHServer(t)

	tun, err := Open(context.Background(), Config{Host: bastionAddr, User: user, Password: password})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tun.Close()

	conn, err := tun.DialContext(context.Background(), "tcp", echoAddr)
	if err != nil {
		t.Fatalf("DialContext through tunnel: %v", err)
	}
	defer conn.Close()

	want := "hello through the tunnel"
	if _, err := conn.Write([]byte(want)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(want))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("expected echo %q, got %q", want, buf)
	}
}

func TestTunnelRejectsWrongPassword(t *testing.T) {
	bastionAddr, user, _ := startSSHServer(t)
	_, err := Open(context.Background(), Config{Host: bastionAddr, User: user, Password: "wrong"})
	if err == nil {
		t.Fatal("expected an error for a wrong password")
	}
}

func TestTunnelRejectsHostKeyMismatch(t *testing.T) {
	bastionAddr, user, password := startSSHServer(t)
	_, err := Open(context.Background(), Config{
		Host: bastionAddr, User: user, Password: password,
		HostKeyFingerprint: "SHA256:not-the-real-fingerprint",
	})
	if err == nil {
		t.Fatal("expected an error for a pinned host key mismatch")
	}
}

func TestTunnelPrivateKeyAuthSucceeds(t *testing.T) {
	echoAddr := startEchoServer(t)
	privatePEM, pubKey := generateTestKeyPair(t)
	bastionAddr, user := startSSHServerWithKey(t, pubKey)

	tun, err := Open(context.Background(), Config{Host: bastionAddr, User: user, PrivateKeyPEM: string(privatePEM)})
	if err != nil {
		t.Fatalf("Open with private key: %v", err)
	}
	defer tun.Close()

	conn, err := tun.DialContext(context.Background(), "tcp", echoAddr)
	if err != nil {
		t.Fatalf("DialContext through tunnel: %v", err)
	}
	defer conn.Close()

	want := "hello via key auth"
	if _, err := conn.Write([]byte(want)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(want))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("expected echo %q, got %q", want, buf)
	}
}

func TestTunnelPrivateKeyWithPassphraseAuthSucceeds(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating client key: %v", err)
	}
	const passphrase = "hunter2"
	block, err := ssh.MarshalPrivateKeyWithPassphrase(key, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("marshaling encrypted private key: %v", err)
	}
	privatePEM := pem.EncodeToMemory(block)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	bastionAddr, user := startSSHServerWithKey(t, signer.PublicKey())

	tun, err := Open(context.Background(), Config{
		Host: bastionAddr, User: user,
		PrivateKeyPEM:        string(privatePEM),
		PrivateKeyPassphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("Open with passphrase-protected key: %v", err)
	}
	tun.Close()
}

func TestTunnelRejectsMalformedPrivateKey(t *testing.T) {
	// authMethod parses PrivateKeyPEM before any network dial happens, so
	// the target host doesn't need to be reachable for this to surface a
	// parsing error rather than a connection error.
	_, err := Open(context.Background(), Config{
		Host: "127.0.0.1:1", User: "someone",
		PrivateKeyPEM: "not a valid PEM key",
	})
	if err == nil {
		t.Fatal("expected an error for a malformed private key")
	}
	if !strings.Contains(err.Error(), "parsing SSH private key") {
		t.Fatalf("expected a private-key parsing error, got: %v", err)
	}
}

func TestTunnelRejectsMissingCredentials(t *testing.T) {
	_, err := Open(context.Background(), Config{Host: "127.0.0.1:1", User: "someone"})
	if err == nil {
		t.Fatal("expected an error when neither password nor private key is configured")
	}
	if !strings.Contains(err.Error(), "requires a password or private key") {
		t.Fatalf("expected a missing-credential error, got: %v", err)
	}
}
