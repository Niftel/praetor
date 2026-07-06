package db

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/praetordev/praetor/pkg/env"
)

const defaultDSN = "postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable"

// Connect opens and verifies a Postgres connection to the given DSN. Prefer this
// from cmd/*/main.go with an explicitly-resolved DSN so the connection string is
// visible at the composition root rather than read from env deep in this package.
func Connect(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DB: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}
	log.Println("Connected to database")
	return db, nil
}

// InitDB connects using DATABASE_URL (falling back to the local dev default).
// Retained for the existing mains; new code should resolve the DSN and call
// Connect directly.
func InitDB() (*sqlx.DB, error) {
	return Connect(env.String("DATABASE_URL", defaultDSN))
}
