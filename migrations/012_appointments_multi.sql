-- Allow multiple appointments per type (up to 10 enforced in application).

ALTER TABLE project_appointments
  DROP CONSTRAINT IF EXISTS project_appointments_project_id_appt_type_key;

CREATE INDEX IF NOT EXISTS idx_project_appointments_project_type_sched
  ON project_appointments (project_id, appt_type, scheduled_at DESC);
