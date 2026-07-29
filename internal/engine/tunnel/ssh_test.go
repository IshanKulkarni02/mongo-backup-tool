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
	return startSSHServerWithDialDelay(t, 0)
}

// startSSHServerWithDialDelay is startSSHServer with a configurable delay
// inserted before the bastion dials the requested target for each
// direct-tcpip channel — used to simulate a slow/congested bastion whose
// channel-open round trip outlasts a caller's context deadline.
func startSSHServerWithDialDelay(t *testing.T, dialDelay time.Duration) (addr, user, password string) {
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
			go handleSSHConn(conn, cfg, dialDelay)
		}
	}()
	return ln.Addr().String(), user, password
}

func handleSSHConn(conn net.Conn, cfg *ssh.ServerConfig, dialDelay time.Duration) {
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
		if dialDelay > 0 {
			time.Sleep(dialDelay)
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

// startTrackedListener runs a TCP listener standing in for "the database"
// on the far side of the tunnel, whose accepted connections are watched
// rather than echoed: each one blocks on a read (which only returns once
// the peer closes its side) and then signals on the returned channel.
// This lets a test observe, from the target side, whether a connection
// opened through the tunnel is ever actually torn down.
func startTrackedListener(t *testing.T) (addr string, closed <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting tracked listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan struct{}, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var buf [1]byte
				c.Read(buf[:]) // blocks until the peer closes (or errors)
				ch <- struct{}{}
			}(conn)
		}
	}()
	return ln.Addr().String(), ch
}

// TestDialContextClosesLateSucceedingConnWhenCallerAbandoned is the
// regression test for #74: DialContext raced its background t.client.Dial
// call against ctx.Done() using a buffered result channel that nobody
// would ever read from again once the caller gave up. If the dial
// completed successfully after the caller already abandoned it (a bastion
// slow enough that ctx's deadline elapses before the channel-open round
// trip finishes), the resulting net.Conn — a live SSH channel — was never
// closed by anyone: silently leaked for the lifetime of the Tunnel.
//
// This is verified from the far side of the tunnel: startTrackedListener
// stands in for "the database" and reports when its accepted connection
// is closed. The bastion is configured to delay each channel-open by
// longer than the caller's context deadline, so DialContext is guaranteed
// to return via ctx.Done() while the dial is still in flight. If the fix
// works, the eventually-successful (but abandoned) dial's conn gets closed
// as soon as it completes, which propagates through the SSH-forwarded
// connection and closes the tracked listener's side too — observed here
// within a generous bound. Before the fix, nothing ever closes it.
func TestDialContextClosesLateSucceedingConnWhenCallerAbandoned(t *testing.T) {
	targetAddr, targetClosed := startTrackedListener(t)
	bastionAddr, user, password := startSSHServerWithDialDelay(t, 300*time.Millisecond)

	tun, err := Open(context.Background(), Config{Host: bastionAddr, User: user, Password: password})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tun.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = tun.DialContext(ctx, "tcp", targetAddr)
	if err == nil {
		t.Fatal("expected DialContext to return an error once its context expired")
	}

	select {
	case <-targetClosed:
		// The abandoned-but-succeeded dial's connection was closed once
		// it completed — no leak.
	case <-time.After(3 * time.Second):
		t.Fatal("connection opened by an abandoned DialContext call was never closed — leaked")
	}
}
