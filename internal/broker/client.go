package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/filelock"
)

// FindRunDir decides which broker an agent should talk to: DBHELM_RUN_DIR if
// set, else the nearest `.dbhelm` folder at or above start (the workspace the
// VS Code extension manages), else a per-user default.
func FindRunDir(start string) string {
	if v := os.Getenv("DBHELM_RUN_DIR"); v != "" {
		return v
	}
	dir, err := filepath.Abs(start)
	if err == nil {
		for {
			if st, err := os.Stat(filepath.Join(dir, ".dbhelm")); err == nil && st.IsDir() {
				return filepath.Join(dir, ".dbhelm", "run")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if cfg, err := config.Dir(); err == nil {
		return filepath.Join(cfg, "run")
	}
	return ""
}

// LogPathFor is where the ship's log lives for a run directory: beside it.
func LogPathFor(runDir string) string {
	return filepath.Join(filepath.Dir(runDir), "logbook.jsonl")
}

// LoadInfo reads a published broker.json.
func LoadInfo(runDir string) (Info, error) {
	data, err := os.ReadFile(InfoPath(runDir))
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

// Client talks to a running broker with one role's token.
type Client struct {
	Info  Info
	Token string
	http  *http.Client
}

// NewAgentClient authenticates with the agent token published in broker.json.
func NewAgentClient(info Info) *Client {
	return &Client{Info: info, Token: info.AgentToken, http: &http.Client{}}
}

// NewOperatorClient authenticates with the operator token handed over at spawn.
func NewOperatorClient(info Info) *Client {
	return &Client{Info: info, Token: info.OperatorToken, http: &http.Client{}}
}

func (c *Client) base() string { return "http://127.0.0.1:" + strconv.Itoa(c.Info.Port) }

// Call invokes a method. A transport failure is returned as an error; a
// failure the broker reports is in the envelope.
func (c *Client) Call(ctx context.Context, method string, params any) (Envelope, error) {
	body, err := json.Marshal(Request{Method: method, Params: mustRaw(params)})
	if err != nil {
		return Envelope{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/rpc", bytes.NewReader(body))
	if err != nil {
		return Envelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Envelope{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("broker returned an unreadable response (HTTP %d)", resp.StatusCode)
	}
	return env, nil
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// Alive reports whether the broker answers a ping.
func (c *Client) Alive(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	env, err := c.Call(ctx, "ping", nil)
	return err == nil && env.OK
}

// Connect returns an agent client for the broker serving runDir. With
// autostart it launches a headless broker (reads only; writes are refused
// because no operator is attached) when none is running.
func Connect(ctx context.Context, runDir, exe string, autostart bool) (*Client, error) {
	if runDir == "" {
		return nil, fmt.Errorf("cannot locate the DBHelm run directory")
	}
	if info, err := LoadInfo(runDir); err == nil {
		if c := NewAgentClient(info); c.Alive(ctx) {
			return c, nil
		}
	}
	if !autostart {
		return nil, fmt.Errorf("no DBHelm broker is running for %s", runDir)
	}
	if err := StartHeadless(ctx, exe, runDir); err != nil {
		return nil, err
	}
	info, err := LoadInfo(runDir)
	if err != nil {
		return nil, err
	}
	return NewAgentClient(info), nil
}

// StartHeadless launches `serve --headless` detached and waits until it
// answers. A lock keeps two simultaneous callers from starting two brokers.
func StartHeadless(ctx context.Context, exe, runDir string) error {
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	return filelock.With(filepath.Join(runDir, "start.lock"), 15*time.Second, time.Minute, "starting the DBHelm broker", func() error {
		if info, err := LoadInfo(runDir); err == nil && NewAgentClient(info).Alive(ctx) {
			return nil
		}
		cmd := exec.Command(exe, "serve", "--headless", "--run-dir", runDir, "--idle", "15m")
		detach(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting the DBHelm broker: %w", err)
		}
		go cmd.Wait()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if info, err := LoadInfo(runDir); err == nil && info.PID == cmd.Process.Pid && NewAgentClient(info).Alive(ctx) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return fmt.Errorf("the DBHelm broker did not start within 10s")
	})
}
