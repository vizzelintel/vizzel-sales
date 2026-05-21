-- Add email verification support for users and OTP workflow.

BEGIN;

ALTER TABLE users
ADD COLUMN IF NOT EXISTS email_verified_at timestamptz;

CREATE TABLE IF NOT EXISTS email_verifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email text NOT NULL,
  otp_hash text NOT NULL,
  expires_at timestamptz NOT NULL,
  attempts int NOT NULL DEFAULT 0,
  resend_available_at timestamptz NOT NULL,
  verified_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_verifications_user_email_created
ON email_verifications (user_id, email, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_email_verifications_expires
ON email_verifications (expires_at);

-- Keep existing users usable in production. New/changed emails will require re-verification.
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, NOW())
WHERE COALESCE(email, '') <> '';

COMMIT;
