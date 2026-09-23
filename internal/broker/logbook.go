package broker

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogEntry is one line of the ship's log: who asked for what, and what
// DBHelm did about it. It is an audit trail, so it records statements but
// never credentials or result rows.
type LogEntry struct {
	Time       string `json:"ts"`
	Actor      string `json:"actor"` // agent | operator | autopilot | broker
	Event      string `json:"event"` // e.g. query, write.requested, write.approved, write.done
	Connection string `json:"connection,omitempty"`
	Database   string `json:"database,omitempty"`
	Statement  string `json:"statement,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Outcome    string `json:"outcome,omitempty"` // ok | refused | failed | pending
	Code       string `json:"code,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

const maxLoggedStatement = 2000

// Logbook appends entries to a JSONL file. A nil or unwritable logbook never
// fails a request: an audit write error is reported once on stderr.
type Logbook struct {
	mu   sync.Mutex
	path string
	warn sync.Once
}

func NewLogbook(path string) *Logbook { return &Logbook{path: path} }

func (l *Logbook) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logbook) Add(e LogEntry) {
	if l == nil || l.path == "" {
		return
	}
	e.Time = time.Now().UTC().Format(time.RFC3339)
	if len(e.Statement) > maxLoggedStatement {
		e.Statement = e.Statement[:maxLoggedStatement] + "…"
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err == nil {
		var f *os.File
		if f, err = os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, err = f.Write(append(line, '\n'))
			f.Close()
		}
	}
	if err != nil {
		l.warn.Do(func() { os.Stderr.WriteString("dbhelm: cannot write ship's log: " + err.Error() + "\n") })
	}
}

// Tail returns the newest limit entries, oldest first.
func (l *Logbook) Tail(limit int) []LogEntry {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []LogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e LogEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}
