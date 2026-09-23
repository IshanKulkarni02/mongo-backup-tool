package broker

// The broker resolves connections by engine id, so every engine must be
// registered no matter which binary embeds it.
import (
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mongodb"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mysql"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/postgres"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)
