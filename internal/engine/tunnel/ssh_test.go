package tunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
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
