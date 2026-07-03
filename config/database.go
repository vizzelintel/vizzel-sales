package config

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

// DatabaseURL returns the Postgres connection string.
// Prefers DATABASE_URL; falls back to legacy SUPABASE_DB_URL during migration.
func DatabaseURL() string {
	if u := strings.TrimSpace(os.Getenv("DATABASE_URL")); u != "" {
		return u
	}
	return strings.TrimSpace(os.Getenv("SUPABASE_DB_URL"))
}

// InitDB connects to PostgreSQL. Direct connections use default prepared statements;
// Supabase pooler URLs (port 6543) still enable simple protocol automatically.
func InitDB() {
	url := DatabaseURL()
	if url == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatalf("Database config failed: %v", err)
	}

	if usesSupabasePooler(url) {
		poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Database ping failed: %v", err)
	}

	DB = pool
	log.Println("Connected to PostgreSQL")
}

func usesSupabasePooler(url string) bool {
	return strings.Contains(url, ":6543/") ||
		strings.Contains(url, "pooler.supabase.com")
}

// PingDB returns nil when the database is reachable.
func PingDB(ctx context.Context) error {
	if DB == nil {
		return pgx.ErrNoRows
	}
	return DB.Ping(ctx)
}
