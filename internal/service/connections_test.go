package service

import (
	"path/filepath"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
)

func TestSetConnectionPassword(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", filepath.Join(t.TempDir(), "cfg"))
	if err := config.Update(func(c *config.Config) error {
		c.Connections = []config.Connection{
			{Name: "pg", Engine: "postgres", URI: "postgres://bob@db:5432/app?sslmode=disable"},
			{Name: "my", Engine: "mysql", URI: "bob@tcp(db:3306)/app"},
			{Name: "mymore", Engine: "mysql", URI: "bob:old@tcp(db:3306)/app"},
			{Name: "nouser", Engine: "postgres", URI: "postgres://db:5432/app"},
			{Name: "file", Engine: "sqlite", URI: "/tmp/x.db"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	get := func(n string) string {
		cfg, _ := config.Load()
		c, _ := cfg.Find(n)
		return c.URI
	}
	if err := SetConnectionPassword("pg", "p@ss/w:rd"); err != nil {
		t.Fatal(err)
	}
	if got := get("pg"); got != "postgres://bob:p%40ss%2Fw%3Ard@db:5432/app?sslmode=disable" {
		t.Errorf("pg = %q", got)
	}
	if err := SetConnectionPassword("my", "s3$cret"); err != nil {
		t.Fatal(err)
	}
	if got := get("my"); got != "bob:s3$cret@tcp(db:3306)/app" {
		t.Errorf("my = %q", got)
	}
	if err := SetConnectionPassword("mymore", "new"); err != nil || get("mymore") != "bob:new@tcp(db:3306)/app" {
		t.Errorf("mymore = %q (%v)", get("mymore"), err)
	}
	for _, n := range []string{"nouser", "file", "missing"} {
		if err := SetConnectionPassword(n, "x"); err == nil {
			t.Errorf("%s: expected an error", n)
		}
	}
}
