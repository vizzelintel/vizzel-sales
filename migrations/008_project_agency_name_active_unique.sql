-- One active project per agency name (case/space-insensitive).
-- Re-create allowed only after the previous row is status = reject.

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_agency_name_active
  ON projects (LOWER(TRIM(agency_name)))
  WHERE status IS DISTINCT FROM 'reject';
