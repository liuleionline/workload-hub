package main

import (
	"path/filepath"
	"testing"
)

func TestLoginBackgroundSeedsAreIdempotent(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.seedDefaults(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM login_backgrounds").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("background seed count = %d, want 2", count)
	}
	var privateAssets int
	if err := db.QueryRow("SELECT COUNT(*) FROM login_backgrounds WHERE asset_path LIKE 'login-bg-%'").Scan(&privateAssets); err != nil {
		t.Fatal(err)
	}
	if privateAssets != 0 {
		t.Fatalf("fresh database seeded %d private photo assets", privateAssets)
	}
}
