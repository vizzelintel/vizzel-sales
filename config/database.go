package config

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

func InitDB() {
	url := os.Getenv("SUPABASE_DB_URL")
	if url == "" {
		log.Fatal("SUPABASE_DB_URL is not set")
	}

	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatalf("Database config failed: %v", err)
	}

	// Supabase transaction-mode pooler (port 6543) cannot maintain
	// prepared statement state across connections. Simple protocol
	// sends plain SQL text and avoids the 42P05 "already exists" errors.
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Database ping failed: %v", err)
	}

	DB = pool
	log.Println("Connected to Supabase PostgreSQL")
}

func GetDB() *pgxpool.Pool {
	return DB
}
