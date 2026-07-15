package handlers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"vizzel-backend/config"
)

// requireTestDB points config.DB at a real Postgres instance for tests that
// exercise cross-company authorization (IDOR) bugs, which cannot be verified
// meaningfully against mocked rows. Start a database with:
//
//	docker compose -f docker-compose.dev.yml up -d postgres
//	./scripts/migrate.sh   # or run migrations/*.sql in order
//
// and export DATABASE_URL before running `go test ./handlers/...`. When no
// database is reachable the test is skipped rather than failed.
func requireTestDB(t *testing.T) {
	t.Helper()
	if config.DB != nil {
		return
	}
	url := config.DatabaseURL()
	if url == "" {
		url = "postgres://vizzel:vizzel_dev@127.0.0.1:5433/vizzel_sales?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("skipping DB-backed test: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping DB-backed test: database not reachable (%v). Start it with "+
			"`docker compose -f docker-compose.dev.yml up -d postgres` and run migrations first.", err)
	}
	config.DB = pool
}

// testCompany inserts a throwaway company row and registers its cleanup.
func testCompany(t *testing.T, ctx context.Context, name, inviteCode string) string {
	t.Helper()
	var id string
	err := config.DB.QueryRow(ctx,
		`INSERT INTO companies (name, type, invite_code) VALUES ($1, 'dealer', $2) RETURNING id::text`,
		name, inviteCode,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test company: %v", err)
	}
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM companies WHERE id = $1::uuid`, id)
	})
	return id
}

// testUser inserts a throwaway user row and registers its cleanup.
func testUser(t *testing.T, ctx context.Context, lineID, role, companyID string) string {
	t.Helper()
	var id string
	err := config.DB.QueryRow(ctx,
		`INSERT INTO users (line_id, full_name, first_name, last_name, role, company_id)
		 VALUES ($1, 'Test User', 'Test', 'User', $2, NULLIF($3,'')::uuid) RETURNING id::text`,
		lineID, role, companyID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, id)
	})
	return id
}

// testProject inserts a throwaway project row and registers its cleanup.
func testProject(t *testing.T, ctx context.Context, companyID, agencyName string) string {
	t.Helper()
	var id string
	err := config.DB.QueryRow(ctx,
		`INSERT INTO projects (company_id, agency_name) VALUES (NULLIF($1,'')::uuid, $2) RETURNING id::text`,
		companyID, agencyName,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test project: %v", err)
	}
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM projects WHERE id = $1::uuid`, id)
	})
	return id
}

// uniqueSuffix keeps fixture names/line_ids unique across parallel test runs.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
