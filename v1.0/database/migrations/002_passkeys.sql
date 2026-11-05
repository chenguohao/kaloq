-- Credential history is retained after revocation. No application private keys.
CREATE TABLE staff_passkeys (
    id text PRIMARY KEY,
    staff_id uuid NOT NULL REFERENCES staff_users(id),
    auth_epoch bigint NOT NULL,
    rp_id text NOT NULL,
    label text NOT NULL CHECK (length(label) BETWEEN 1 AND 80),
    credential_id bytea NOT NULL,
    credential jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    UNIQUE (rp_id, credential_id)
);
CREATE INDEX staff_passkeys_owner ON staff_passkeys(staff_id);

CREATE TABLE staff_passkey_challenges (
    id text PRIMARY KEY,
    staff_id uuid NOT NULL REFERENCES staff_users(id),
    auth_epoch bigint NOT NULL,
    session_hash text NOT NULL REFERENCES staff_sessions(token_hash) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('register', 'credential_test')),
    label text NOT NULL,
    session_data jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE INDEX staff_passkey_challenges_expiry ON staff_passkey_challenges(expires_at);
