package tui

import (
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
)

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	switch m.screen {
	case screenDeps:
		b.WriteString(m.viewDeps())
	case screenConnections:
		b.WriteString(m.viewConnections())
	case screenAddConnection:
		b.WriteString(m.viewAddConnection())
	case screenDatabases:
		b.WriteString(m.viewDatabases())
	case screenMenu:
		b.WriteString(m.viewMenu())
	case screenMessageInput:
		b.WriteString(m.viewMessageInput())
	case screenList:
		b.WriteString(m.viewList())
	case screenConfirm:
		b.WriteString(m.viewConfirm())
	case screenProgress:
		b.WriteString(m.viewProgress())
	case screenResult:
		b.WriteString(m.viewResult())
	}
	return b.String()
}

func header(title string) string {
	return titleStyle.Render(title) + "\n\n"
}

func footer(help string) string {
	return "\n" + helpStyle.Render(help) + "\n"
}

func (m Model) viewDeps() string {
	var b strings.Builder
	b.WriteString(header("mongobak — checking dependencies"))
	for _, s := range m.depStatuses {
		if s.Installed {
			b.WriteString(okStyle.Render("  ✓ "+s.Dependency.Name) + " " + mutedStyle.Render(s.Version) + "\n")
		} else {
			b.WriteString(errorStyle.Render("  ✗ "+s.Dependency.Name) + " " + mutedStyle.Render(s.Dependency.Description) + "\n")
		}
	}

	if depmanager.AllInstalled(m.depStatuses) {
		b.WriteString("\nLoading...\n")
		return b.String()
	}

	b.WriteString("\nmongodump/mongorestore are needed for classic backups (snapshots don't need them).\n\n")
	choices := m.depChoices()
	for i, c := range choices {
		b.WriteString(cursorPrefix(i == m.depCursor) + c + "\n")
	}

	if m.depShowManual {
		b.WriteString("\n" + labelStyle.Render("Manual install:") + "\n")
		for _, line := range depmanager.ManualInstructions() {
			b.WriteString("  " + line + "\n")
		}
	}

	if len(m.depLog) > 0 {
		b.WriteString("\n" + labelStyle.Render("Install log:") + "\n")
		for _, line := range m.depLog {
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString(footer("↑/↓ select · enter choose · esc quit"))
	return b.String()
}

func (m Model) viewConnections() string {
	var b strings.Builder
	b.WriteString(header("Connections"))
	if m.connErr != "" {
		b.WriteString(errorStyle.Render("Error: "+m.connErr) + "\n\n")
	}
	if len(m.connections) == 0 {
		b.WriteString(mutedStyle.Render("No connections yet. Press 'a' to add one.") + "\n")
	}
	for i, c := range m.connections {
		b.WriteString(cursorPrefix(i == m.connCursor) + c.Name + "  " + mutedStyle.Render(redactURIForTUI(c.URI)) + "\n")
	}
	b.WriteString(footer("↑/↓ select · enter choose · a add · esc quit"))
	return b.String()
}

func (m Model) viewAddConnection() string {
	var b strings.Builder
	b.WriteString(header("Add connection"))
	b.WriteString(labelStyle.Render("Name") + "\n" + m.nameInput.View() + "\n\n")
	b.WriteString(labelStyle.Render("URI") + "\n" + m.uriInput.View() + "\n")
	if m.addErr != "" {
		b.WriteString("\n" + errorStyle.Render(m.addErr) + "\n")
	}
	b.WriteString(footer("tab switch field · enter save · esc cancel"))
	return b.String()
}

func (m Model) viewDatabases() string {
	var b strings.Builder
	b.WriteString(header("Databases on " + m.connection.Name))
	if m.dbErr != "" {
		b.WriteString(errorStyle.Render("Error: "+m.dbErr) + "\n\n")
	}

	if m.dbTyping {
		b.WriteString(labelStyle.Render("Database name") + "\n" + m.dbInput.View() + "\n")
		b.WriteString(footer("enter confirm · esc cancel"))
		return b.String()
	}

	for i, d := range m.databases {
		b.WriteString(cursorPrefix(i == m.dbCursor) + d + "\n")
	}
	b.WriteString(cursorPrefix(m.dbCursor == len(m.databases)) + mutedStyle.Render("[type a database name]") + "\n")
	b.WriteString(footer("↑/↓ select · enter choose · esc back"))
	return b.String()
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(header(m.connection.Name + " / " + m.database))
	for i, item := range m.menuItems {
		b.WriteString(cursorPrefix(i == m.menuCursor) + item.label + "\n")
	}
	b.WriteString(footer("↑/↓ select · enter choose · esc back"))
	return b.String()
}

func (m Model) viewMessageInput() string {
	var b strings.Builder
	b.WriteString(header("Snapshot message"))
	b.WriteString(m.messageInput.View() + "\n")
	b.WriteString(footer("enter create snapshot · esc cancel"))
	return b.String()
}

func (m Model) viewList() string {
	var b strings.Builder
	title, help := m.listTitleAndHelp()
	b.WriteString(header(title))
	if m.listErr != "" {
		b.WriteString(errorStyle.Render("Error: "+m.listErr) + "\n\n")
	}

	switch m.listPurpose {
	case listPurposeBackupView, listPurposeBackupRestore:
		if len(m.backups) == 0 {
			b.WriteString(mutedStyle.Render("No backups yet.") + "\n")
		}
		for i, bk := range m.backups {
			db := bk.Database
			if db == "" {
				db = "(all)"
			}
			line := fmt.Sprintf("%s  %-15s  %-10s  %s", shortID(bk.ID), db, humanSizeStr(bk.SizeBytes), bk.CreatedAt)
			b.WriteString(cursorPrefix(i == m.listCursor) + line + "\n")
		}
	default:
		if len(m.snapshots) == 0 {
			b.WriteString(mutedStyle.Render("No snapshots yet.") + "\n")
		}
		for i, s := range m.snapshots {
			tag := ""
			if len(s.Tags) > 0 {
				tag = "  [" + strings.Join(s.Tags, ", ") + "]"
			}
			line := fmt.Sprintf("%s  %-25s  %d docs  %s%s", shortID(s.ID), s.CreatedAt, s.DocCount, s.Message, tag)
			b.WriteString(cursorPrefix(i == m.listCursor) + line + "\n")
		}
	}

	b.WriteString(footer(help))
	return b.String()
}

func (m Model) listTitleAndHelp() (title, help string) {
	switch m.listPurpose {
	case listPurposeSnapshotView:
		return "Snapshot history", "↑/↓ scroll · esc back"
	case listPurposeSnapshotRestore:
		return "Pick a snapshot to restore", "↑/↓ select · enter restore · esc back"
	case listPurposeSnapshotDiffLive:
		return "Pick a snapshot to diff against the live database", "↑/↓ select · enter diff · esc back"
	case listPurposeBackupView:
		return "Backups", "↑/↓ scroll · esc back"
	case listPurposeBackupRestore:
		return "Pick a backup to restore", "↑/↓ select · enter restore · esc back"
	}
	return "", "esc back"
}

func (m Model) viewConfirm() string {
	var b strings.Builder
	b.WriteString(header("Confirm"))
	b.WriteString(m.confirmPrompt + "\n")
	b.WriteString(footer("y confirm · n/esc cancel"))
	return b.String()
}

func (m Model) viewProgress() string {
	return header(m.progressText)
}

func (m Model) viewResult() string {
	var b strings.Builder
	if m.resultIsErr {
		b.WriteString(header("Error"))
		for _, line := range m.resultLines {
			b.WriteString(errorStyle.Render(line) + "\n")
		}
	} else {
		b.WriteString(header("Done"))
		for _, line := range m.resultLines {
			b.WriteString(okStyle.Render("✓ ") + line + "\n")
		}
	}
	b.WriteString(footer("enter/esc to continue"))
	return b.String()
}

func redactURIForTUI(uri string) string {
	// Same redaction as the CLI's cmd.redactURI, duplicated to avoid
	// tui -> cmd import cycle (cmd imports tui to launch it).
	at := strings.LastIndex(uri, "@")
	scheme := strings.Index(uri, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return uri
	}
	colon := strings.Index(uri[scheme+3:at], ":")
	if colon < 0 {
		return uri
	}
	return uri[:scheme+3+colon+1] + "****" + uri[at:]
}
