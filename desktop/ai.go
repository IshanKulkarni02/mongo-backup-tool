package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/ai"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// aiRequestTimeout bounds a single AI completion so a stalled provider
// can't hold server-side resources indefinitely even if the user never
// clicks Cancel.
const aiRequestTimeout = 2 * time.Minute

// AISettingsInfo is AI settings as shown to the frontend — never the raw
// API key, only whether one is set.
type AISettingsInfo struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	OllamaHost string `json:"ollamaHost"`
	HasAPIKey  bool   `json:"hasApiKey"`
}

// GetAISettings returns the saved AI provider/model configuration.
func (a *App) GetAISettings() (AISettingsInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return AISettingsInfo{}, err
	}
	return AISettingsInfo{
		ProviderID: cfg.AI.ProviderID, Model: cfg.AI.Model,
		OllamaHost: cfg.AI.OllamaHost, HasAPIKey: cfg.AI.HasAPIKey,
	}, nil
}

// SaveAISettings persists the provider/model choice. apiKey is optional —
// pass "" to leave any previously stored key untouched, or a new value to
// replace it (stored in the system keychain, not in config.json).
func (a *App) SaveAISettings(providerID, model, ollamaHost, apiKey string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.AI.ProviderID = providerID
	cfg.AI.Model = model
	cfg.AI.OllamaHost = ollamaHost
	if apiKey != "" {
		if err := config.SetAIAPIKey(apiKey); err != nil {
			return fmt.Errorf("saving API key: %w", err)
		}
		cfg.AI.HasAPIKey = true
	}
	return config.Save(cfg)
}

func (a *App) providerFromSettings() (ai.Provider, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	var apiKey string
	if cfg.AI.HasAPIKey {
		apiKey, _ = config.AIAPIKey()
	}
	return ai.NewProvider(ai.Config{
		ProviderID: cfg.AI.ProviderID, Model: cfg.AI.Model, APIKey: apiKey, OllamaHost: cfg.AI.OllamaHost,
	})
}

// runAIStream starts a completion and streams it to the frontend via
// "ai:stream:<id>" events ({"delta": string, "done": bool} or {"error":
// string, "done": true}), returning the stream ID immediately. The stream
// ID doubles as a cancelable-job ID — CancelJob(streamID) stops it, the
// same binding the SQL query view already uses, since jobManager's cancel
// registry is just a string-keyed map with no other job bookkeeping
// required to use it.
func (a *App) runAIStream(messages []ai.Message) (string, error) {
	provider, err := a.providerFromSettings()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiRequestTimeout)
	ch, err := provider.Complete(ctx, messages)
	if err != nil {
		cancel()
		return "", err
	}
	streamID := uuid.NewString()
	eventName := "ai:stream:" + streamID
	a.jobs.mu.Lock()
	a.jobs.cancels[streamID] = cancel
	a.jobs.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			a.jobs.mu.Lock()
			delete(a.jobs.cancels, streamID)
			a.jobs.mu.Unlock()
		}()
		emittedTerminal := false
		for chunk := range ch {
			if chunk.Err != nil {
				runtime.EventsEmit(a.ctx, eventName, map[string]any{"error": chunk.Err.Error(), "done": true})
				emittedTerminal = true
				break
			}
			runtime.EventsEmit(a.ctx, eventName, map[string]any{"delta": chunk.Delta, "done": chunk.Done})
			if chunk.Done {
				emittedTerminal = true
			}
		}
		if !emittedTerminal {
			// The channel closed without a terminal chunk — the only way
			// that happens is Cancel(streamID) firing mid-stream (the
			// provider's own goroutine always sends Done or Err before
			// closing otherwise). Tell the frontend so it stops showing
			// "generating...".
			msg := "canceled"
			if err := ctx.Err(); err != nil {
				msg = err.Error()
			}
			runtime.EventsEmit(a.ctx, eventName, map[string]any{"error": msg, "done": true})
		}
	}()
	return streamID, nil
}

// describeSchema renders a table's columns/FKs as compact text for an AI
// prompt — not real DDL (see internal/codegen in a later phase for that),
// just enough shape for the model to reason about column names/types.
func describeSchema(schema engine.TableSchema) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "table %s(", schema.Name)
	for i, c := range schema.Columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s %s", c.Name, c.DataType)
		if c.IsPK {
			sb.WriteString(" PK")
		}
	}
	sb.WriteString(")")
	for _, fk := range schema.ForeignKeys {
		fmt.Fprintf(&sb, "; FK %s.%s -> %s.%s", schema.Name, fk.Column, fk.RefTable, fk.RefColumn)
	}
	return sb.String()
}

func (a *App) describeTables(connectionName, database string, tables []string) ([]string, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		schema, err := sess.TableSchema(context.Background(), database, t)
		if err != nil {
			continue // a table we can't introspect just isn't included in context
		}
		out = append(out, describeSchema(schema))
	}
	return out, nil
}

// GenerateSQL turns a natural-language request into a SQL statement,
// scoped to the given tables' schemas (RAG-lite — an explicit allowlist,
// not the whole database). Streams via ai:stream:<id>; the frontend
// inserts the result into the editor and never auto-executes it — Safe
// Mode still applies when the user runs it.
func (a *App) GenerateSQL(connectionName, database, dialect string, tables []string, request string) (string, error) {
	schemas, err := a.describeTables(connectionName, database, tables)
	if err != nil {
		return "", err
	}
	return a.runAIStream(ai.BuildNLToSQLPrompt(dialect, schemas, request))
}

// GenerateAggregation is GenerateSQL's MongoDB counterpart, producing a
// pipeline instead of a SQL statement.
func (a *App) GenerateAggregation(collection, schemaSample, request string) (string, error) {
	return a.runAIStream(ai.BuildNLToAggregationPrompt(collection, schemaSample, request))
}

// ExplainWithAI translates a raw EXPLAIN output into plain-English tuning
// advice.
func (a *App) ExplainWithAI(dialect, query, explainOutput string) (string, error) {
	return a.runAIStream(ai.BuildExplainTuningPrompt(dialect, query, explainOutput))
}

// FixSQLError asks the model to correct a statement given the database's
// own error message and the relevant schema.
func (a *App) FixSQLError(connectionName, database, dialect, query, errMsg string, tables []string) (string, error) {
	schemas, err := a.describeTables(connectionName, database, tables)
	if err != nil {
		return "", err
	}
	return a.runAIStream(ai.BuildErrorFixPrompt(dialect, query, errMsg, schemas))
}

// GenerateMockData asks the model for realistic INSERT statements for a
// table, informed by its actual column names/types.
func (a *App) GenerateMockData(connectionName, database, dialect, table string, rowCount int) (string, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return "", err
	}
	schema, err := sess.TableSchema(context.Background(), database, table)
	release()
	if err != nil {
		return "", err
	}
	return a.runAIStream(ai.BuildMockDataPrompt(dialect, table, describeSchema(schema), rowCount))
}

// CheckOllama reports whether a local Ollama instance is installed/running.
func (a *App) CheckOllama() depmanager.OllamaStatus {
	return depmanager.CheckOllama(context.Background())
}

// InstallOllama installs Ollama via the OS package manager as a background
// job; output lines are collected into the job's Result on completion.
func (a *App) InstallOllama() string {
	j := a.jobs.start("ollama-install")
	go func() {
		var output []string
		err := depmanager.AutoInstallOllama(context.Background(), func(line string) {
			output = append(output, line)
			a.jobs.progress(j.ID, "install", int64(len(output)), 0, line)
		})
		a.jobs.finish(j.ID, err, output)
	}()
	return j.ID
}

// ListOllamaModels returns models already pulled into the local instance.
// host may be "" for the default.
func (a *App) ListOllamaModels(host string) ([]ai.OllamaModel, error) {
	return ai.ListModels(context.Background(), host)
}

// PullOllamaModel downloads a model as a cancelable background job,
// streaming progress via "ai:pull-progress" events.
func (a *App) PullOllamaModel(host, model string) string {
	j := a.jobs.start("ollama-pull")
	ctx, cancel := context.WithCancel(context.Background())
	a.jobs.mu.Lock()
	a.jobs.cancels[j.ID] = cancel
	a.jobs.mu.Unlock()
	go func() {
		err := ai.PullModel(ctx, host, model, func(p ai.PullProgress) {
			a.jobs.progress(j.ID, p.Status, p.Completed, p.Total, "")
		})
		a.jobs.mu.Lock()
		delete(a.jobs.cancels, j.ID)
		a.jobs.mu.Unlock()
		a.jobs.finish(j.ID, err, nil)
	}()
	return j.ID
}
