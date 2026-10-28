// Package postgres contiene los adaptadores de persistencia sobre PostgreSQL.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // driver pgx para database/sql (sqlc)
)

// Options controla el pool de conexiones a PostgreSQL.
type Options struct {
	// MaxOpenConns es el número máximo de conexiones abiertas. 0 usa el
	// default de database/sql (ilimitado); en producción conviene acotarlo.
	MaxOpenConns int
	// MaxIdleConns es el número de conexiones mantenidas en el pool.
	MaxIdleConns int
	// ConnMaxLifetime es la vida máxima de una conexión antes de reciclarse.
	ConnMaxLifetime time.Duration
}

// Open abre un pool de conexiones a PostgreSQL usando DATABASE_URL
// (ej. postgres://user:pass@localhost:5432/junkbox_db?sslmode=disable).
func Open(ctx context.Context, databaseURL string, opts Options) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: abrir pool: %w", err)
	}

	if opts.MaxOpenConns > 0 {
		db.SetMaxOpenConns(opts.MaxOpenConns)
	}
	if opts.MaxIdleConns > 0 {
		db.SetMaxIdleConns(opts.MaxIdleConns)
	}
	if opts.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(opts.ConnMaxLifetime)
	}

	// Validar que el datasource realmente funcione antes de devolverlo.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return db, nil
}
