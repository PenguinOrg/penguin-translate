package cache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	mu    sync.RWMutex
	mem   map[string]string
	db    *sql.DB
	model string
}

func Open(model string) (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "window-translate", "cache.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS translations (
			hash TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			translation TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
	`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{mem: make(map[string]string), db: db, model: model}, nil
}

func (s *Store) SetModel(model string) {
	s.mu.Lock()
	s.model = model
	s.mu.Unlock()
}

func (s *Store) key(source string) string {
	h := sha256.Sum256([]byte(s.model + "\n" + normalize(source)))
	return hex.EncodeToString(h[:])
}

func normalize(t string) string {
	var b []byte
	space := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !space {
				b = append(b, ' ')
				space = true
			}
			continue
		}
		space = false
		b = append(b, c)
	}
	for len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && b[0] == ' ' {
		b = b[1:]
	}
	return string(b)
}

func (s *Store) Get(source string) (string, bool) {
	k := s.key(source)
	s.mu.RLock()
	if v, ok := s.mem[k]; ok {
		s.mu.RUnlock()
		return v, true
	}
	s.mu.RUnlock()

	var tr string
	err := s.db.QueryRow(`SELECT translation FROM translations WHERE hash = ?`, k).Scan(&tr)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	s.mem[k] = tr
	s.mu.Unlock()
	return tr, true
}

func (s *Store) Put(source, translation string) error {
	k := s.key(source)
	norm := normalize(source)
	s.mu.Lock()
	s.mem[k] = translation
	s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO translations (hash, source, translation, model, created_at) VALUES (?, ?, ?, ?, ?)`,
		k, norm, translation, s.model, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("cache write: %w", err)
	}
	return nil
}
