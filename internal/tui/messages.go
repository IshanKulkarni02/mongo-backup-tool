package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/depmanager"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/tunnel"
	"github.com/IshanKulkarni02/dbhelm/internal/humansize"
	"github.com/IshanKulkarni02/dbhelm/internal/mongotools"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

// openSQLSession opens a one-shot engine.SQLSession for a saved connection —
// the TUI has no long-lived session cache, so callers must invoke the
// returned release func when done.
func openSQLSession(conn config.Connection) (engine.SQLSession, func(), error) {
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, nil, err
	}
	connCfg := engine.ConnConfig{
		Name: conn.Name, URI: conn.URI, ReadOnly: conn.ReadOnly,
		TenantSessionVar: conn.TenantSessionVar, TenantValue: conn.TenantValue,
	}
	if conn.SSHHost != "" {
		knownHosts, err := config.SSHKnownHostsPath()
		if err != nil {
			return nil, nil, err
		}
		connCfg.SSHTunnel = &tunnel.Config{
			Host:           conn.SSHHost,
			User:           conn.SSHUser,
			Password:       conn.SSHPassword,
			PrivateKeyPEM:  conn.SSHPrivateKey,
			KnownHostsPath: knownHosts,
		}
	}
	sess, err := eng.Open(context.Background(), connCfg)
	if err != nil {
		return nil, nil, err
	}
	ss, ok := sess.(engine.SQLSession)
	if !ok {
		sess.Close(context.Background())
		return nil, nil, fmt.Errorf("connection %q isn't a SQL database", conn.Name)
	}
	return ss, func() { ss.Close(context.Background()) }, nil
}

type depsCheckedMsg struct{ statuses []depmanager.Status }
type depsInstallLineMsg struct{ line string }
type depsInstallDoneMsg struct{ err error }

type desktopAppResolvedMsg struct {
	launched bool // true: the app was already installed and just launched
	err      error
}

type connectionsLoadedMsg struct {
	conns []config.Connection
	err   error
}
type connectionSavedMsg struct{ err error }

type databasesLoadedMsg struct {
	dbs []string
	err error
}

type snapshotsLoadedMsg struct {
	items []snapshot.Summary
	err   error
}
type backupsLoadedMsg struct {
	items []store.Backup
	err   error
}

type actionDoneMsg struct {
	lines []string
	err   error
}

func checkDepsCmd() tea.Msg {
	return depsCheckedMsg{statuses: depmanager.Check()}
}

// persistLauncherChoice saves the terminal-vs-desktop choice so the chooser
// only appears once. Best-effort: a write failure just means it's asked
// again next run, not worth surfacing as an error on top of the choice
// itself.
func persistLauncherChoice(choice string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	cfg.Launcher.Choice = choice
	_ = config.Save(cfg)
}

func chooseTerminalCmd() tea.Msg {
	persistLauncherChoice("terminal")
	return depsCheckedMsg{statuses: depmanager.Check()}
}

func chooseDesktopAppCmd() tea.Msg {
	persistLauncherChoice("desktop")
	status := depmanager.CheckDesktopApp(context.Background())
	if status.Installed {
		err := depmanager.LaunchDesktopApp(context.Background(), status.Path)
		return desktopAppResolvedMsg{launched: true, err: err}
	}
	err := depmanager.AutoInstallDesktopApp(context.Background(), func(line string) {
		if programRef != nil {
			programRef.Send(depsInstallLineMsg{line: line})
		}
	})
	return desktopAppResolvedMsg{launched: false, err: err}
}

// programRef is set once by Run() before the program starts, so a running
// tea.Cmd (which has no direct handle to the program) can stream output
// back via Send while a long-running install command executes.
var programRef *tea.Program

func autoInstallDepsCmd() tea.Msg {
	err := depmanager.AutoInstall(context.Background(), func(line string) {
		if programRef != nil {
			programRef.Send(depsInstallLineMsg{line: line})
		}
	})
	return depsInstallDoneMsg{err: err}
}

func loadConnectionsCmd() tea.Msg {
	cfg, err := config.Load()
	if err != nil {
		return connectionsLoadedMsg{err: err}
	}
	return connectionsLoadedMsg{conns: cfg.Connections}
}

func saveConnectionCmd(name, uri, engineID string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return connectionSavedMsg{err: err}
		}
		// Preserve any tenant value already set for an existing connection
		// of the same name, same as the CLI's `connection add` — only the
		// desktop app's SwitchTenant sets this, and re-saving here
		// shouldn't clear it.
		tenantValue := ""
		if existing, ok := cfg.Find(name); ok {
			tenantValue = existing.TenantValue
		}
		cfg.Upsert(config.Connection{
			Name:        name,
			URI:         uri,
			Engine:      engineID,
			TenantValue: tenantValue,
			CreatedAt:   time.Now().Format(time.RFC3339),
		})
		return connectionSavedMsg{err: config.Save(cfg)}
	}
}

func loadDatabasesCmd(uri string) tea.Cmd {
	return func() tea.Msg {
		dbs, err := mongotools.TestConnection(uri)
		return databasesLoadedMsg{dbs: dbs, err: err}
	}
}

func loadSnapshotsCmd(connName, db string) tea.Cmd {
	return func() tea.Msg {
		items, err := snapshot.Log(connName, db)
		return snapshotsLoadedMsg{items: items, err: err}
	}
}

func loadBackupsCmd() tea.Msg {
	dir, err := config.BackupsDir()
	if err != nil {
		return backupsLoadedMsg{err: err}
	}
	idx, err := store.Load(dir)
	if err != nil {
		return backupsLoadedMsg{err: err}
	}
	return backupsLoadedMsg{items: idx.Backups}
}

func createSnapshotCmd(conn config.Connection, db, message string) tea.Cmd {
	return func() tea.Msg {
		eng, err := engine.Lookup(conn.EngineID())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if eng.Capabilities().SQL {
			sess, release, err := openSQLSession(conn)
			if err != nil {
				return actionDoneMsg{err: err}
			}
			defer release()
			res, err := snapshot.CreateSQL(context.Background(), snapshot.SQLCreateOptions{
				Connection: conn.Name, Database: db, Message: message, Session: sess,
			})
			if err != nil {
				return actionDoneMsg{err: err}
			}
			lines := []string{
				fmt.Sprintf("Snapshot %s created", res.Summary.ID),
				fmt.Sprintf("%d rows (%d new objects)", res.Summary.DocCount, res.Summary.NewObjects),
			}
			if len(res.SkippedTables) > 0 {
				lines = append(lines, fmt.Sprintf("Skipped %d table(s) with no primary key", len(res.SkippedTables)))
			}
			return actionDoneMsg{lines: lines}
		}

		res, err := snapshot.Create(snapshot.CreateOptions{Connection: conn.Name, URI: conn.URI, Database: db, Message: message})
		if err != nil {
			return actionDoneMsg{err: err}
		}
		lines := []string{
			fmt.Sprintf("Snapshot %s created", res.Summary.ID),
			fmt.Sprintf("%d docs (%d new objects)", res.Summary.DocCount, res.Summary.NewObjects),
		}
		if !res.Consistent {
			lines = append(lines, "Note: this deployment isn't a replica set, so this was a plain scan rather than a point-in-time-consistent one.")
		}
		return actionDoneMsg{lines: lines}
	}
}

func diffLiveCmd(conn config.Connection, db, snapshotID string) tea.Cmd {
	return func() tea.Msg {
		eng, err := engine.Lookup(conn.EngineID())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		// ScanLive only speaks the Mongo wire protocol; comparing a SQL
		// snapshot against its live database isn't implemented yet.
		if eng.Capabilities().SQL {
			return actionDoneMsg{err: fmt.Errorf("comparing against the live database isn't supported for SQL connections yet")}
		}

		from, err := snapshot.Get(conn.Name, db, snapshotID)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		scope, err := snapshot.OpenScope(conn.Name, db)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		defer scope.Close()

		live, err := snapshot.ScanLive(conn.URI, db)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		defer live.Close()
		diff, err := snapshot.Compare(context.Background(), from, scope.Source(from.ID), live.Manifest, live.Source())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if diff.Empty() {
			return actionDoneMsg{lines: []string{"No differences from the live database."}}
		}
		var lines []string
		for name, cd := range diff.Collections {
			lines = append(lines, fmt.Sprintf("%s: +%d added, ~%d modified, -%d removed", name, cd.AddedCount, cd.ModifiedCount, cd.RemovedCount))
		}
		return actionDoneMsg{lines: lines}
	}
}

func restoreSnapshotCmd(conn config.Connection, db, snapshotID string) tea.Cmd {
	return func() tea.Msg {
		eng, err := engine.Lookup(conn.EngineID())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if eng.Capabilities().SQL {
			sess, release, err := openSQLSession(conn)
			if err != nil {
				return actionDoneMsg{err: err}
			}
			defer release()
			// RestoreSQLWithSafety's error message already says whether it
			// auto-rolled back, so it's passed straight through here.
			result, safety, _, err := snapshot.RestoreSQLWithSafety(context.Background(), snapshot.SQLRestoreOptions{
				SourceConnection: conn.Name,
				SourceDatabase:   db,
				SnapshotID:       snapshotID,
				Session:          sess,
				EngineID:         conn.EngineID(),
				Drop:             true,
			}, conn.Name)
			if err != nil {
				return actionDoneMsg{err: err}
			}
			lines := []string{fmt.Sprintf("Restored %d rows across %d table(s)", result.DocsWritten, len(result.Collections))}
			if safety != nil {
				lines = append(lines, fmt.Sprintf("Safety snapshot taken first: %s", safety.Summary.ID))
			}
			return actionDoneMsg{lines: lines}
		}

		// RestoreWithSafety's error message already says whether it
		// auto-rolled back, so it's passed straight through here.
		result, safety, _, err := snapshot.RestoreWithSafety(snapshot.RestoreOptions{
			SourceConnection: conn.Name,
			SourceDatabase:   db,
			SnapshotID:       snapshotID,
			TargetURI:        conn.URI,
			Drop:             true,
		}, conn.Name)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		lines := []string{fmt.Sprintf("Restored %d docs across %d collection(s)", result.DocsWritten, len(result.Collections))}
		if safety != nil {
			lines = append(lines, fmt.Sprintf("Safety snapshot taken first: %s", safety.Summary.ID))
		}
		return actionDoneMsg{lines: lines}
	}
}

func createBackupCmd(connName, uri, db string) tea.Cmd {
	return func() tea.Msg {
		id, err := runBackup(connName, uri, db)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{lines: []string{fmt.Sprintf("Backup %s created", id)}}
	}
}

func restoreBackupCmd(connName, uri, backupID string) tea.Cmd {
	return func() tea.Msg {
		if err := runBackupRestore(connName, uri, backupID); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{lines: []string{"Backup restored"}}
	}
}

func humanSizeStr(n int64) string { return humansize.Format(n) }
