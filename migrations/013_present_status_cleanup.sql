-- Present is no longer a pipeline status; merge into register.
UPDATE projects SET status = 'register' WHERE status = 'present';

-- Remove duplicate appointment rows (same project, type, time, note).
DELETE FROM project_appointments a
USING project_appointments b
WHERE a.id < b.id
  AND a.project_id = b.project_id
  AND a.appt_type = b.appt_type
  AND a.scheduled_at = b.scheduled_at
  AND COALESCE(a.note, '') = COALESCE(b.note, '')
  AND COALESCE(a.present_type, '') = COALESCE(b.present_type, '');
