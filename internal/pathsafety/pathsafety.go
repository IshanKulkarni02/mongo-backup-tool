// Package pathsafety helps keep user-controlled strings (connection names,
// database names, stored file names) from escaping a fixed directory when
// used to build a filesystem path.
package pathsafety

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// unsafeFileNameChars matches any character that isn't safe to place
// directly in a single path segment. In particular it strips path
// separators (and their Windows-style backslash form), which is what makes
// "../" sequences in user-controlled input harmless: a component with no
// separator characters can never escape the directory it's later joined
// against with filepath.Join.
var unsafeFileNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SanitizeComponent makes a user-controlled string (a connection name or
// database name) safe to embed in a file name.
func SanitizeComponent(s string) string {
	return unsafeFileNameChars.ReplaceAllString(s, "_")
}

// SafeJoin joins dir with fileName and verifies the result is still inside
// dir, guarding against file names that predate sanitization (or a
// hand-edited/corrupted index) rather than relying solely on fileName
// having been sanitized when it was first written.
func SafeJoin(dir, fileName string) (string, error) {
	path := filepath.Join(dir, fileName)
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to use file name %q: escapes %s", fileName, dir)
	}
	return path, nil
}
