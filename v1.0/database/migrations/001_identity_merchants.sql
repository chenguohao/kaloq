CREATE TABLE staff_users (
 id uuid PRIMARY KEY, login_name text NOT NULL UNIQUE CHECK(login_name=lower(login_name)),
 display_name text NOT NULL, password_hash text NOT NULL, status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 auth_epoch bigint NOT NULL DEFAULT 1, roles text[] NOT NULL,
 totp_secret_enc bytea, totp_last_step bigint NOT NULL DEFAULT -1, created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (roles <@ ARRAY['operator','finance','supervisor','eo','admin','root']::text[] AND cardinality(roles)>0)
);
CREATE TABLE staff_role_assignments (
 id bigserial PRIMARY KEY, staff_id uuid NOT NULL REFERENCES staff_users(id), roles text[] NOT NULL,
 assignment_version bigint NOT NULL, actor_id uuid REFERENCES staff_users(id), created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(staff_id,assignment_version)
);
CREATE TABLE staff_sessions (
 token_hash text PRIMARY KEY, staff_id uuid NOT NULL REFERENCES staff_users(id), auth_epoch bigint NOT NULL,
 stage text NOT NULL CHECK(stage IN ('enroll','totp','full')), csrf_token text NOT NULL,
 enrollment_secret_enc bytea, attempts int NOT NULL DEFAULT 0,
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX staff_sessions_staff ON staff_sessions(staff_id);
CREATE TABLE recovery_codes (staff_id uuid NOT NULL REFERENCES staff_users(id), code_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(staff_id,code_hash));
CREATE TABLE auth_rate_limits (key text PRIMARY KEY, window_at timestamptz NOT NULL, attempts int NOT NULL);
CREATE TABLE audit_events (
 id bigserial PRIMARY KEY, actor_id uuid REFERENCES staff_users(id), action text NOT NULL,
 target_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit events are append only'; END $$;
CREATE TRIGGER audit_append_only BEFORE UPDATE OR DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE merchants (
 id uuid PRIMARY KEY, merchant_code text NOT NULL UNIQUE, legal_name text NOT NULL,
 contact_enc bytea NOT NULL,
 service_status text NOT NULL DEFAULT 'draft' CHECK(service_status IN ('draft','active','paused')),
 row_version bigint NOT NULL DEFAULT 1, created_by uuid NOT NULL REFERENCES staff_users(id),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE merchant_profile_versions (
 merchant_id uuid NOT NULL REFERENCES merchants(id), version bigint NOT NULL,
 legal_name text NOT NULL, contact_enc bytea NOT NULL,
 actor_id uuid NOT NULL REFERENCES staff_users(id), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(merchant_id,version)
);
