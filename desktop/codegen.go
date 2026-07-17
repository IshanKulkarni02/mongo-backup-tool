package main

import (
	"context"
	"fmt"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/codegen"
)

// GenerateAPISchema exports one table's schema as an API/type artifact.
// format is "openapi" | "typescript" | "pydantic".
func (a *App) GenerateAPISchema(connectionName, database, table, format string) (string, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return "", err
	}
	schema, err := sess.TableSchema(context.Background(), database, table)
	release()
	if err != nil {
		return "", err
	}

	switch format {
	case "openapi":
		return codegen.GenerateOpenAPI(schema, ""), nil
	case "typescript":
		return codegen.GenerateTypeScript(schema, ""), nil
	case "pydantic":
		return codegen.GeneratePydantic(schema, ""), nil
	default:
		return "", fmt.Errorf("unknown format %q (use openapi, typescript, or pydantic)", format)
	}
}
