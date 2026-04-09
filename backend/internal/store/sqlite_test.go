package store

import "testing"

func TestOpenCreatesCoreTables(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"local_device", "trusted_peers", "conversations", "messages", "transfers"} {
		if !db.HasTable(table) {
			t.Fatalf("expected table %s", table)
		}
	}
}
