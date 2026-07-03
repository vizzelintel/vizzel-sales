-- Fresh PostgreSQL schema for self-hosted Vizzel Sales (final state after 001–014).
-- Run once on empty database: scripts/migrate.sh

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- companies
-- ---------------------------------------------------------------------------
CREATE TABLE companies (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    type        text NOT NULL DEFAULT 'dealer',
    address     text DEFAULT '',
    tax_id      text DEFAULT '',
    invite_code text UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT NOW(),
    updated_at  timestamptz NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    line_id           text NOT NULL UNIQUE,
    full_name         text NOT NULL DEFAULT '',
    first_name        text DEFAULT '',
    last_name         text DEFAULT '',
    phone             text DEFAULT '',
    email             text DEFAULT '',
    region            text DEFAULT '',
    role              text NOT NULL DEFAULT 'dealer'
                        CHECK (role IN ('dealer', 'support', 'admin')),
    company_id        uuid REFERENCES companies(id) ON DELETE SET NULL,
    invite_code_used  text DEFAULT '',
    email_verified_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_company_id ON users (company_id);
CREATE INDEX idx_users_line_id ON users (line_id);

-- ---------------------------------------------------------------------------
-- projects
-- ---------------------------------------------------------------------------
CREATE TABLE projects (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id        uuid REFERENCES companies(id) ON DELETE SET NULL,
    agency_name       text NOT NULL,
    agency_type       text DEFAULT '',
    region            text DEFAULT '',
    contact_person    text DEFAULT '',
    contact_position  text DEFAULT '',
    contact_phone     text DEFAULT '',
    status            text NOT NULL DEFAULT 'register'
                        CHECK (status IN (
                            'register', 'quotation', 'tor',
                            'contract', 'closed', 'reject'
                        )),
    status_note       text DEFAULT '',
    reject_reason     text DEFAULT '',
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    last_activity_at  timestamptz,
    appointment_date  timestamptz,
    appointment_note  text DEFAULT '',
    calendar_event_id text DEFAULT '',
    present_type      text DEFAULT '',
    detail_note       text DEFAULT '',
    auto_reject_at    timestamptz,
    lark_record_id    text DEFAULT ''
);

CREATE UNIQUE INDEX idx_projects_agency_name_active
    ON projects (LOWER(TRIM(agency_name)))
    WHERE status IS DISTINCT FROM 'reject';

CREATE INDEX idx_projects_created_at_desc ON projects (created_at DESC);
CREATE INDEX idx_projects_company_created ON projects (company_id, created_at DESC);
CREATE INDEX idx_projects_status_created ON projects (status, created_at DESC);
CREATE INDEX idx_projects_auto_reject_active
    ON projects (auto_reject_at)
    WHERE auto_reject_at IS NOT NULL
      AND status NOT IN ('contract', 'closed', 'reject');

CREATE INDEX idx_projects_lark_record_id
    ON projects (lark_record_id)
    WHERE lark_record_id IS NOT NULL AND lark_record_id <> '';

-- ---------------------------------------------------------------------------
-- project_status_logs
-- ---------------------------------------------------------------------------
CREATE TABLE project_status_logs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    from_status text DEFAULT '',
    to_status   text DEFAULT '',
    changed_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    note        text DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_status_logs_project ON project_status_logs (project_id);

-- ---------------------------------------------------------------------------
-- documents
-- ---------------------------------------------------------------------------
CREATE TABLE documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    doc_type    text NOT NULL
                    CHECK (doc_type IN (
                        'quotation_support', 'quotation_dealer',
                        'tor_support', 'tor_dealer',
                        'contract', 'closing',
                        'site_survey', 'attachment'
                    )),
    file_url    text NOT NULL DEFAULT '',
    file_name   text DEFAULT '',
    file_size   bigint DEFAULT 0,
    mime_type   text DEFAULT '',
    uploaded_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_documents_single_per_type
    ON documents (project_id, doc_type)
    WHERE doc_type NOT IN ('site_survey', 'attachment');

CREATE INDEX idx_documents_project_doc_type ON documents (project_id, doc_type);

-- ---------------------------------------------------------------------------
-- project_appointments
-- ---------------------------------------------------------------------------
CREATE TABLE project_appointments (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    appt_type         text NOT NULL CHECK (appt_type IN ('present', 'demo', 'site_survey')),
    scheduled_at      timestamptz NOT NULL,
    note              text DEFAULT '',
    present_type      text DEFAULT '',
    calendar_event_id text DEFAULT '',
    meet_setup        text DEFAULT '',
    meet_link         text DEFAULT '',
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    updated_at        timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_appointments_project_id ON project_appointments (project_id);
CREATE INDEX idx_project_appointments_project_type_sched
    ON project_appointments (project_id, appt_type, scheduled_at DESC);

-- ---------------------------------------------------------------------------
-- email_verifications
-- ---------------------------------------------------------------------------
CREATE TABLE email_verifications (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email               text NOT NULL,
    otp_hash            text NOT NULL,
    expires_at          timestamptz NOT NULL,
    attempts            int NOT NULL DEFAULT 0,
    resend_available_at timestamptz NOT NULL,
    verified_at         timestamptz,
    created_at          timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_email_verifications_user_email_created
    ON email_verifications (user_id, email, created_at DESC);

CREATE INDEX idx_email_verifications_expires ON email_verifications (expires_at);

COMMIT;
