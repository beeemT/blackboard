// Package sqlite implements the transactional registry boundary. Entity payloads
// are versioned JSON; tables and keys preserve independent identities and history.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/beeemT/blackboard/internal/domain"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

type Store struct{ db *sql.DB }

var tables = []string{"topics", "sessions", "work", "links", "activity", "observations"}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path required")
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(abs), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: abs}
	db, e := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	tx, e := db.Begin()
	if e != nil {
		db.Close()
		return nil, e
	}
	defer tx.Rollback()
	var version int
	if e = tx.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	if version > 1 {
		db.Close()
		return nil, fmt.Errorf("database schema %d is newer than supported version 1", version)
	}
	for _, table := range tables {
		if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS ` + table + ` (key TEXT PRIMARY KEY, payload TEXT NOT NULL)`); e != nil {
			db.Close()
			return nil, e
		}
	}
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, result TEXT NOT NULL); PRAGMA user_version=1;`); e != nil {
		db.Close()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func load[T any](ctx context.Context, q queryer, table string) ([]T, error) {
	rows, e := q.QueryContext(ctx, `SELECT payload FROM `+table+` ORDER BY rowid`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []T{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var x T
		if e = json.Unmarshal([]byte(b), &x); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}
func read(ctx context.Context, q queryer) (domain.Snapshot, error) {
	v := domain.Empty()
	var e error
	if v.Topics, e = load[domain.Topic](ctx, q, "topics"); e != nil {
		return v, e
	}
	if v.Sessions, e = load[domain.Session](ctx, q, "sessions"); e != nil {
		return v, e
	}
	if v.Work, e = load[domain.Work](ctx, q, "work"); e != nil {
		return v, e
	}
	if v.Links, e = load[domain.Link](ctx, q, "links"); e != nil {
		return v, e
	}
	if v.Activity, e = load[domain.Activity](ctx, q, "activity"); e != nil {
		return v, e
	}
	v.Observations, e = load[domain.Observation](ctx, q, "observations")
	return v, e
}
func (s *Store) Read(ctx context.Context) (domain.Snapshot, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return domain.Snapshot{}, e
	}
	defer tx.Rollback()
	return read(ctx, tx)
}
func save[T any](ctx context.Context, tx *sql.Tx, table string, items []T, key func(T) string) error {
	for _, x := range items {
		b, e := json.Marshal(x)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO `+table+` (key,payload) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload WHERE payload<>excluded.payload`, key(x), string(b)); e != nil {
			return e
		}
	}
	return nil
}
func persist(ctx context.Context, tx *sql.Tx, v domain.Snapshot) error {
	// Links are the only removable entities; retain all topic/activity history.
	if _, err := tx.ExecContext(ctx, "DELETE FROM links"); err != nil {
		return err
	}

	if e := save(ctx, tx, "topics", v.Topics, func(x domain.Topic) string { return x.ID }); e != nil {
		return e
	}
	if e := save(ctx, tx, "sessions", v.Sessions, func(x domain.Session) string { return x.ID }); e != nil {
		return e
	}
	if e := save(ctx, tx, "work", v.Work, func(x domain.Work) string { return x.ID }); e != nil {
		return e
	}
	if e := save(ctx, tx, "links", v.Links, func(x domain.Link) string { b, _ := json.Marshal(x); return string(b) }); e != nil {
		return e
	}
	if e := save(ctx, tx, "activity", v.Activity, func(x domain.Activity) string { return x.ID }); e != nil {
		return e
	}
	return save(ctx, tx, "observations", v.Observations, func(x domain.Observation) string { b, _ := json.Marshal([]string{x.Source, x.Key}); return string(b) })
}
func (s *Store) Apply(ctx context.Context, id, hash string, fn func(*domain.Snapshot) (any, error)) (json.RawMessage, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var oldHash, result string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM operations WHERE id=?`, id).Scan(&oldHash, &result)
	if e == nil {
		if oldHash != hash {
			return nil, fmt.Errorf("conflict: operation_id reused with different payload")
		}
		return json.RawMessage(result), nil
	}
	if e != sql.ErrNoRows {
		return nil, e
	}
	v, e := read(ctx, tx)
	if e != nil {
		return nil, e
	}
	out, e := fn(&v)
	if e != nil {
		return nil, e
	}
	res, e := json.Marshal(out)
	if e != nil {
		return nil, e
	}
	if e = persist(ctx, tx, v); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO operations VALUES(?,?,?)`, id, hash, string(res)); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return res, nil
}
