package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/humansize"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
)

type depsCheckedMsg struct{ statuses []depmanager.Status }
type depsInstallLineMsg struct{ line string }
type depsInstallDoneMsg struct{ err error }

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

func saveConnectionCmd(name, uri string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return connectionSavedMsg{err: err}
		}
		cfg.Upsert(config.Connection{Name: name, URI: uri, CreatedAt: time.Now().Format(time.RFC3339)})
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

func createSnapshotCmd(connName, uri, db, message string) tea.Cmd {
	return func() tea.Msg {
		res, err := snapshot.Create(snapshot.CreateOptions{Connection: connName, URI: uri, Database: db, Message: message})
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

func diffLiveCmd(connName, uri, db, snapshotID string) tea.Cmd {
	return func() tea.Msg {
		from, err := snapshot.Get(connName, db, snapshotID)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		scope, err := snapshot.OpenScope(connName, db)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		defer scope.Close()

		live, err := snapshot.ScanLive(uri, db)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		diff, err := snapshot.Compare(from, scope.Source(from.ID), live.Manifest, live.Source())
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if diff.Empty() {
			return actionDoneMsg{lines: []string{"No differences from the live database."}}
		}
		var lines []string
		for name, cd := range diff.Collections {
			lines = append(lines, fmt.Sprintf("%s: +%d added, ~%d modified, -%d removed", name, len(cd.Added), len(cd.Modified), len(cd.Removed)))
		}
		return actionDoneMsg{lines: lines}
	}
}

func restoreSnapshotCmd(connName, uri, db, snapshotID string) tea.Cmd {
	return func() tea.Msg {
		result, safety, err := snapshot.RestoreWithSafety(snapshot.RestoreOptions{
			SourceConnection: connName,
			SourceDatabase:   db,
			SnapshotID:       snapshotID,
			TargetURI:        uri,
			Drop:             true,
		}, connName)
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
