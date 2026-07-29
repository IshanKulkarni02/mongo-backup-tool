package engine

import "regexp"

// sessionVarNameRe restricts a tenant session-variable name to identifier
// characters (plus dots, for Postgres' namespaced GUCs like
// "app.current_tenant"). The variable's *value* is always sent as a
// parameterized query argument, never interpolated — this only guards the
// name, which SQL doesn't let you parameterize, against breaking the
// SET/set_config statement it's spliced into.
var sessionVarNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// ValidSessionVarName reports whether name is safe to splice into a SET
// statement.
func ValidSessionVarName(name string) bool {
	return sessionVarNameRe.MatchString(name)
}
