-- Per-project appointment slots (present, demo, site_survey) — one row each per project.

CREATE TABLE IF NOT EXISTS project_appointments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  appt_type text NOT NULL CHECK (appt_type IN ('present', 'demo', 'site_survey')),
  scheduled_at timestamptz NOT NULL,
  note text DEFAULT '',
  present_type text DEFAULT '',
  calendar_event_id text DEFAULT '',
  created_by uuid REFERENCES users(id),
  created_at timestamptz DEFAULT now(),
  updated_at timestamptz DEFAULT now(),
  UNIQUE (project_id, appt_type)
);

CREATE INDEX IF NOT EXISTS idx_project_appointments_project_id
  ON project_appointments (project_id);

-- Backfill from legacy single-column fields on projects
INSERT INTO project_appointments (project_id, appt_type, scheduled_at, note, present_type, calendar_event_id)
SELECT p.id, p.status, p.appointment_date, COALESCE(p.appointment_note, ''), COALESCE(p.present_type, ''), COALESCE(p.calendar_event_id, '')
FROM projects p
WHERE p.appointment_date IS NOT NULL
  AND p.status IN ('present', 'demo', 'site_survey')
ON CONFLICT (project_id, appt_type) DO NOTHING;
