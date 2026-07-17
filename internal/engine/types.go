package engine

// NamespaceInfo is one collection's (or table's) summary, shown in browser
// trees.
type NamespaceInfo struct {
	Name        string `json:"name"`
	DocCount    int64  `json:"docCount"`
	StorageSize int64  `json:"storageSize"`
}

// DocQuery is a filtered, sorted, paginated document read. FilterJSON and
// SortJSON are Extended JSON text (or empty for "match everything" / "no
// explicit sort").
type DocQuery struct {
	Database   string
	Namespace  string
	FilterJSON string
	SortJSON   string
	Skip       int
	Limit      int
}

// DocPage is one page of documents, each rendered as relaxed Extended JSON
// (human-readable — plain numbers/strings where unambiguous, unlike the
// snapshot engine's canonical mode which favors deterministic hashing over
// readability).
type DocPage struct {
	Documents []string `json:"documents"`
	Total     int64    `json:"total"`
	Skip      int      `json:"skip"`
	Limit     int      `json:"limit"`
}

// IndexInfo is one index, rendered for display (not round-tripped back into
// a create call — CreateIndex takes its own fresh keysJSON/unique input).
type IndexInfo struct {
	Name     string `json:"name"`
	KeysJSON string `json:"keysJson"`
	Unique   bool   `json:"unique"`
}

// Column describes one column in a table's introspected schema.
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
	IsPK     bool   `json:"isPk"`
}

// ForeignKey describes one declared FK constraint: Column in this table
// references RefTable.RefColumn.
type ForeignKey struct {
	Column    string `json:"column"`
	RefTable  string `json:"refTable"`
	RefColumn string `json:"refColumn"`
}

// TableSchema is one table's introspected shape, cached per session so
// autocomplete and FK-hyperlink rendering don't re-query on every cell.
type TableSchema struct {
	Name        string       `json:"name"`
	Columns     []Column     `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
}

// CellType tags how a SQL result value should be rendered — a database's
// native type (bytea, JSONB, vector, geometry, timestamps) is mapped to
// one of these so the frontend can pick a type-appropriate widget instead
// of stringifying everything.
type CellType string

const (
	CellNull   CellType = "null"
	CellBool   CellType = "bool"
	CellNumber CellType = "number"
	CellString CellType = "string"
	CellJSON   CellType = "json"
	CellBinary CellType = "binary"
	CellDate   CellType = "date"
)

// Cell is one query-result value, JSON-safe for the Wails bridge. Display
// is always a ready-to-render string; Raw carries the JSON-marshalable
// value for cells (like CellJSON) where the frontend may want to parse
// further.
type Cell struct {
	Type    CellType `json:"type"`
	Display string   `json:"display"`
	Raw     any      `json:"raw,omitempty"`
}

// SQLResult is a page of rows from a SQL query, columns in query order.
type SQLResult struct {
	Columns []string          `json:"columns"`
	Rows    []map[string]Cell `json:"rows"`
	// Total is the row count of this result set, or -1 when unknown
	// (arbitrary ad-hoc SQL isn't re-counted with a second query).
	Total int64 `json:"total"`
}
