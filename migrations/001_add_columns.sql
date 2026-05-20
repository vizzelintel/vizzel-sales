-- Run in Supabase SQL Editor (or psql) before deploying the new backend build.

-- Projects: new sales pipeline fields
ALTER TABLE projects ADD COLUMN IF NOT EXISTS agency_type      text;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS contact_position text;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS reject_reason    text;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS status_note      text;

-- Documents: Supabase Storage upload schema
-- (New columns; existing name/type/url/size columns are kept for compatibility)
ALTER TABLE documents ADD COLUMN IF NOT EXISTS doc_type    text;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS file_url    text;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS uploaded_by uuid REFERENCES users(id);

-- Projects: Google Calendar integration
ALTER TABLE projects ADD COLUMN IF NOT EXISTS appointment_date  timestamptz;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS appointment_note  text;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS calendar_event_id text;

-- Documents: file metadata for validation and display
ALTER TABLE documents ADD COLUMN IF NOT EXISTS file_name text;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS file_size bigint;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS mime_type text;
