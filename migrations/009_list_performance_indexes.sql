-- Indexes for project list, filters, cron auto-reject, and document lookups.

CREATE INDEX IF NOT EXISTS idx_projects_created_at_desc
  ON projects (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_projects_company_created
  ON projects (company_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_projects_status_created
  ON projects (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_projects_auto_reject_active
  ON projects (auto_reject_at)
  WHERE auto_reject_at IS NOT NULL
    AND status NOT IN ('contract', 'closed', 'reject');

CREATE INDEX IF NOT EXISTS idx_documents_project_doc_type
  ON documents (project_id, doc_type);

CREATE INDEX IF NOT EXISTS idx_project_appointments_project
  ON project_appointments (project_id);
