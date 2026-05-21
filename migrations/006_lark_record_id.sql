-- Store Lark Bitable record_id per project for reliable upsert (avoids flaky search).

ALTER TABLE projects ADD COLUMN IF NOT EXISTS lark_record_id text;

CREATE INDEX IF NOT EXISTS idx_projects_lark_record_id
  ON projects (lark_record_id)
  WHERE lark_record_id IS NOT NULL AND lark_record_id <> '';
