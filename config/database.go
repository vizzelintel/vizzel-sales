package config

import (
"context"
"log"
"os"

"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

func InitDB() {
url := os.Getenv("SUPABASE_DB_URL")
if url == "" {
log.Fatal("Database connection failed: SUPABASE_DB_URL environment variable is not set")
}

pool, err := pgxpool.New(context.Background(), url)
if err != nil {
log.Fatalf("Database connection failed: %v", err)
}

if err := pool.Ping(context.Background()); err != nil {
log.Fatalf("Database connection failed: failed to ping database: %v", err)
}

DB = pool
log.Println("Connected to Supabase PostgreSQL")
}

func GetDB() *pgxpool.Pool {
return DB
}
