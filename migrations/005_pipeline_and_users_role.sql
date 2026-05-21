-- Fix user registration role constraint + remove demo/site_survey as pipeline statuses.

BEGIN;

-- User profile columns used by registration
ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS region text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS invite_code_used text;

-- Allow dealer / support / admin (fixes users_role_check on new registrations)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
-- Legacy Supabase schema used member/super_admin; app uses dealer/support/admin.
UPDATE users SET role = 'dealer' WHERE role = 'member';
UPDATE users SET role = 'admin' WHERE role = 'super_admin';
UPDATE users
SET role = 'dealer'
WHERE role IS NULL OR TRIM(role) = ''
   OR role NOT IN ('dealer', 'support', 'admin');
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (
  role IN ('dealer', 'support', 'admin')
);

-- Preserve demo/site_survey appointment dates before normalizing status
INSERT INTO project_appointments (project_id, appt_type, scheduled_at, note, present_type, calendar_event_id)
SELECT p.id, p.status, p.appointment_date, COALESCE(p.appointment_note, ''), COALESCE(p.present_type, ''), COALESCE(p.calendar_event_id, '')
FROM projects p
WHERE p.status IN ('demo', 'site_survey')
  AND p.appointment_date IS NOT NULL
ON CONFLICT (project_id, appt_type) DO NOTHING;

UPDATE projects SET status = 'present' WHERE status IN ('demo', 'site_survey');

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_status_check;
ALTER TABLE projects ADD CONSTRAINT projects_status_check CHECK (
  status IN (
    'register',
    'present',
    'quotation',
    'tor',
    'contract',
    'closed',
    'reject'
  )
);

COMMIT;
