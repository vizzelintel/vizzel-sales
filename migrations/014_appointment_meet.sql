ALTER TABLE project_appointments
  ADD COLUMN IF NOT EXISTS meet_setup text DEFAULT '',
  ADD COLUMN IF NOT EXISTS meet_link text DEFAULT '';
