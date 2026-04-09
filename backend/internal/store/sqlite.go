package store

import "sync"

type DB struct {
	mu     sync.RWMutex
	dsn    string
	tables map[string]struct{}
}

func Open(dsn string) (*DB, error) {
	return &DB{
		dsn: dsn,
		tables: map[string]struct{}{
			"local_device":  {},
			"trusted_peers": {},
			"conversations": {},
			"messages":      {},
			"transfers":     {},
		},
	}, nil
}

func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.dsn = ""
	return nil
}

func (db *DB) HasTable(name string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, ok := db.tables[name]
	return ok
}
