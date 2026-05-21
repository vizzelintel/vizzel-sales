-- One row per (project, doc_type) except site_survey (max 3 enforced in app).

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_single_per_type
  ON documents (project_id, doc_type)
  WHERE doc_type <> 'site_survey';
