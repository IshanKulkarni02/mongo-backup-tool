package tunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// generateTestHostKey returns a fresh SSH public key, standing in for a
// bastion's host key without needing a real network handshake.
func generateTestHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer.PublicKey()
}

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

// startSSHServer runs a minimal in-process SSH server that accepts a fixed
// password and forwards direct-tcpip channels (what ssh.Client.Dial opens)
// to whatever address the client asked for — a stand-in for a real
// bastion host, so the tunnel dialer can be tested without one.
func startSSHServer(t *testing.T) (addr, user, password string) {
	t.Helper()
	user, password = "tunneluser", "s3cret"

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	cfg.AddHostKey(signer)

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
	return ln.Addr().String(), user, password
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

// TestTunnelTOFURecordsHostKeyOnFirstConnect is the end-to-end regression
// test for #72: a real Open() call with KnownHostsPath set (no manual
// HostKeyFingerprint) must succeed on first connection to a bastion, and
// must record that bastion's actual key fingerprint to the known-hosts
// file — proving the wiring from Open through hostKeyCallback into
// tofuHostKeyCallback actually runs, not just the callback in isolation.
func TestTunnelTOFURecordsHostKeyOnFirstConnect(t *testing.T) {
	bastionAddr, user, password := startSSHServer(t)
	knownHosts := filepath.Join(t.TempDir(), "known_hosts.json")

	tun, err := Open(context.Background(), Config{
		Host: bastionAddr, User: user, Password: password,
		KnownHostsPath: knownHosts,
	})
	if err != nil {
		t.Fatalf("Open (first connect, TOFU): %v", err)
	}
	tun.Close()

	hosts, err := loadKnownHosts(knownHosts)
	if err != nil {
		t.Fatalf("loadKnownHosts: %v", err)
	}
	if hosts[bastionAddr] == "" {
		t.Fatalf("expected a recorded fingerprint for %s, got %+v", bastionAddr, hosts)
	}

	// A second connection to the same (unchanged) bastion must still
	// succeed against the now-recorded fingerprint.
	tun2, err := Open(context.Background(), Config{
		Host: bastionAddr, User: user, Password: password,
		KnownHostsPath: knownHosts,
	})
	if err != nil {
		t.Fatalf("Open (second connect, same key): %v", err)
	}
	tun2.Close()
}

// TestTOFUHostKeyCallbackTrustsFirstConnection unit-tests
// tofuHostKeyCallback directly: the very first key seen for a host is
// always trusted and recorded.
func TestTOFUHostKeyCallbackTrustsFirstConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts.json")
	key := generateTestHostKey(t)

	cb := tofuHostKeyCallback(path, "bastion.example.com:22")
	if err := cb("bastion.example.com:22", nil, key); err != nil {
		t.Fatalf("first connection should be trusted, got: %v", err)
	}

	hosts, err := loadKnownHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if hosts["bastion.example.com:22"] != ssh.FingerprintSHA256(key) {
		t.Fatalf("recorded fingerprint = %q, want %q", hosts["bastion.example.com:22"], ssh.FingerprintSHA256(key))
	}
}

// TestTOFUHostKeyCallbackAcceptsSameKeyAgain confirms a host that's
// already trusted keeps working on later connections presenting the same
// key — TOFU shouldn't mean "trust once, then always fail."
func TestTOFUHostKeyCallbackAcceptsSameKeyAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts.json")
	key := generateTestHostKey(t)
	cb := tofuHostKeyCallback(path, "bastion.example.com:22")

	if err := cb("bastion.example.com:22", nil, key); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	if err := cb("bastion.example.com:22", nil, key); err != nil {
		t.Fatalf("second connection with the same key should still be trusted, got: %v", err)
	}
}

// TestTOFUHostKeyCallbackRejectsChangedKey is the core MITM-detection
// regression test for #72: once a host's key is trusted, a *different*
// key presented for the same host — exactly what an attacker
// impersonating that host, or a real MITM, would present — must be
// rejected, not silently accepted the way InsecureIgnoreHostKey always
// did.
func TestTOFUHostKeyCallbackRejectsChangedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts.json")
	firstKey := generateTestHostKey(t)
	secondKey := generateTestHostKey(t)
	cb := tofuHostKeyCallback(path, "bastion.example.com:22")

	if err := cb("bastion.example.com:22", nil, firstKey); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	err := cb("bastion.example.com:22", nil, secondKey)
	if err == nil {
		t.Fatal("expected an error when a different key is presented for an already-trusted host")
	}

	// The original (correct) fingerprint must survive the rejected attempt
	// — an attacker's presented key must never overwrite the trusted one.
	hosts, loadErr := loadKnownHosts(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if hosts["bastion.example.com:22"] != ssh.FingerprintSHA256(firstKey) {
		t.Fatalf("trusted fingerprint was overwritten by the rejected connection attempt")
	}
}

// TestTOFUHostKeyCallbackDifferentHostsIndependent confirms trust is
// scoped per-host: trusting one bastion's key must not affect whether a
// different host's key is treated as first-seen.
func TestTOFUHostKeyCallbackDifferentHostsIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts.json")
	keyA := generateTestHostKey(t)
	keyB := generateTestHostKey(t)

	cbA := tofuHostKeyCallback(path, "host-a.example.com:22")
	if err := cbA("host-a.example.com:22", nil, keyA); err != nil {
		t.Fatalf("host A first connection: %v", err)
	}

	cbB := tofuHostKeyCallback(path, "host-b.example.com:22")
	if err := cbB("host-b.example.com:22", nil, keyB); err != nil {
		t.Fatalf("host B first connection (independent of host A's trust): %v", err)
	}

	hosts, err := loadKnownHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if hosts["host-a.example.com:22"] != ssh.FingerprintSHA256(keyA) {
		t.Fatal("host A's fingerprint missing or wrong after host B connected")
	}
	if hosts["host-b.example.com:22"] != ssh.FingerprintSHA256(keyB) {
		t.Fatal("host B's fingerprint missing or wrong")
	}
}
