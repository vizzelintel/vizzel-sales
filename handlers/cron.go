package handlers

import (
	"context"
	"log"
	"time"

	"vizzel-backend/config"
)

func StartAutoRejectCron() {
	log.Println("[CRON] Auto-reject cron started")
	runAutoReject()
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		runAutoReject()
	}
}

func runAutoReject() {
	ctx := context.Background()
	log.Printf("[CRON] Running auto-reject check at %s\n", time.Now().Format("2006-01-02 15:04:05"))

	rows, err := config.DB.Query(ctx, `
		SELECT id::text, COALESCE(status,''), COALESCE(agency_name,'')
		FROM projects
		WHERE auto_reject_at IS NOT NULL
		  AND auto_reject_at < NOW()
		  AND status NOT IN ('contract','closed','reject')
	`)
	if err != nil {
		log.Printf("[CRON] query error: %v\n", err)
		return
	}
	defer rows.Close()

	type proj struct{ id, status, name string }
	var toReject []proj
	for rows.Next() {
		var p proj
		if err := rows.Scan(&p.id, &p.status, &p.name); err == nil {
			toReject = append(toReject, p)
		}
	}
	rows.Close()

	log.Printf("[CRON] Found %d project(s) to auto-reject\n", len(toReject))

	for _, p := range toReject {
		_, err := config.DB.Exec(ctx, `
			UPDATE projects
			SET status           = 'reject',
			    reject_reason    = 'หมดเวลา 90 วัน ไม่มีการอัปเดต',
			    auto_reject_at   = NULL,
			    last_activity_at = NOW()
			WHERE id = $1::uuid
		`, p.id)
		if err != nil {
			log.Printf("[CRON] failed to reject project %s: %v\n", p.id, err)
			continue
		}

		_, _ = config.DB.Exec(ctx, `
			INSERT INTO project_status_logs (project_id, from_status, to_status, note)
			VALUES ($1::uuid, $2, 'reject', 'ปิดอัตโนมัติ: ไม่มีการอัปเดตเกิน 90 วัน')
		`, p.id, p.status)

		log.Printf("[CRON] Auto-rejected project %s (%s)\n", p.id, p.name)

		// Sync rejected project to Lark in background
		pid := p.id
		go func() {
			if proj, err := scanProject(config.DB.QueryRow(ctx,
				`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, pid,
			)); err == nil {
				SyncProjectToLark(proj)
			}
		}()
	}
}
