package mongotools

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
)

// RunResult captures a completed command's output.
type RunResult struct {
	Stdout string
	Stderr string
}

func run(bin string, args []string) (*RunResult, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := &RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return res, fmt.Errorf("%s failed: %w\n%s", filepath.Base(bin), err, res.Stderr)
	}
	return res, nil
}

// DumpOptions configures a mongodump invocation.
type DumpOptions struct {
	URI         string
	Database    string // empty = dump every database in the deployment
	ArchivePath string
}

// Dump runs mongodump, writing a single gzip-compressed archive file.
func Dump(opts DumpOptions) (*RunResult, error) {
	bin, err := Find("mongodump")
	if err != nil {
		return nil, err
	}
	args := []string{"--uri=" + opts.URI, "--archive=" + opts.ArchivePath, "--gzip"}
	if opts.Database != "" {
		args = append(args, "--db="+opts.Database)
	}
	return run(bin, args)
}

// RestoreOptions configures a mongorestore invocation.
type RestoreOptions struct {
	URI         string
	ArchivePath string
	SourceDB    string // database name captured at backup time, "" = all
	TargetDB    string // optional: restore into a different database name
	Drop        bool   // drop existing collections before restoring
}

// Restore runs mongorestore against a gzip archive produced by Dump.
func Restore(opts RestoreOptions) (*RunResult, error) {
	bin, err := Find("mongorestore")
	if err != nil {
		return nil, err
	}
	args := []string{"--uri=" + opts.URI, "--archive=" + opts.ArchivePath, "--gzip"}
	if opts.Drop {
		args = append(args, "--drop")
	}
	if opts.TargetDB != "" && opts.SourceDB != "" && opts.TargetDB != opts.SourceDB {
		args = append(args, "--nsFrom="+opts.SourceDB+".*", "--nsTo="+opts.TargetDB+".*")
	}
	return run(bin, args)
}

// Version returns a tool's --version output.
func Version(base string) (string, error) {
	bin, err := Find(base)
	if err != nil {
		return "", err
	}
	res, err := run(bin, []string{"--version"})
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
