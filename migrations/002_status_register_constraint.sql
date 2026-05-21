-- Normalize legacy status value and enforce canonical status set.
-- Safe to run multiple times.

BEGIN;

UPDATE projects
SET status = 'register'
WHERE status = 'registrator';

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_status_check;

ALTER TABLE projects
ADD CONSTRAINT projects_status_check CHECK (
  status IN (
    'register',
    'present',
    'demo',
    'site_survey',
    'quotation',
    'tor',
    'contract',
    'closed',
    'reject'
  )
);

COMMIT;
