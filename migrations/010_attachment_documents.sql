-- Allow multiple attachment rows per project (max 5 enforced in app).

DROP INDEX IF EXISTS idx_documents_single_per_type;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_single_per_type
  ON documents (project_id, doc_type)
  WHERE doc_type NOT IN ('site_survey', 'attachment');
