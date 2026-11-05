package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "kaloq_v1_") {
		return nil, errors.New("database must use isolated kaloq_v1_ prefix")
	}
	cfg.MaxConns = 10
	return pgxpool.NewWithConfig(ctx, cfg)
}
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return errors.New("no migrations found")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(746201105)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("another migration runner holds the lock")
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(746201105)`)
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, name := range names {
		data, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return e
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		var prior string
		e = conn.QueryRow(ctx, `SELECT sha256 FROM schema_migrations WHERE name=$1`, name).Scan(&prior)
		if e == nil {
			if prior != digest {
				return fmt.Errorf("migration checksum mismatch: %s", name)
			}
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='30s'`); e == nil {
			_, e = tx.Exec(ctx, string(data))
		}
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)`, name, digest)
		}
		if e != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s failed: %w", name, e)
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
