-- These rows record observations. Execution capabilities and credentials are separate.
CREATE TABLE provider_instances (
    id uuid PRIMARY KEY,
    provider_code text NOT NULL,
    instance_code text NOT NULL,
    environment text NOT NULL,
    created_by uuid NOT NULL REFERENCES staff_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment, instance_code)
);

CREATE TABLE provider_bindings (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL REFERENCES merchants(id),
    instance_id uuid NOT NULL REFERENCES provider_instances(id),
    kind text NOT NULL CHECK (kind IN ('enrollment', 'virtual_account')),
    label text NOT NULL,
    account_scope bytea NOT NULL,
    external_key bytea NOT NULL,
    masked_external_id text NOT NULL,
    -- Encrypted upstream account scope, external identifier, and evidence reference.
    details_enc bytea NOT NULL,
    profile_version bigint NOT NULL,
    parent_binding_id uuid,
    parent_kind text GENERATED ALWAYS AS ('enrollment'::text) STORED,
    asset text NOT NULL,
    review_state text NOT NULL DEFAULT 'pending_review' CHECK (review_state = 'pending_review'),
    created_by uuid NOT NULL REFERENCES staff_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (instance_id,kind,account_scope,external_key),
    UNIQUE (merchant_id,id),
    UNIQUE (merchant_id,id,instance_id),
    UNIQUE (merchant_id,instance_id,id,kind),
    FOREIGN KEY (merchant_id,profile_version) REFERENCES merchant_profile_versions(merchant_id,version),
    FOREIGN KEY (merchant_id,instance_id,parent_binding_id,parent_kind)
        REFERENCES provider_bindings(merchant_id,instance_id,id,kind),
    CHECK ((kind = 'enrollment' AND asset = '' AND parent_binding_id IS NULL)
        OR (kind = 'virtual_account' AND asset <> '' AND parent_binding_id IS NOT NULL))
);

CREATE TABLE payment_targets (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL REFERENCES merchants(id),
    kind text NOT NULL CHECK (kind IN ('bank','chain')),
    created_by uuid NOT NULL REFERENCES staff_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (merchant_id,id,kind)
);
CREATE TABLE payment_target_versions (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL,
    target_id uuid NOT NULL,
    request_id uuid NOT NULL,
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    version bigint NOT NULL CHECK (version > 0),
    kind text NOT NULL,
    label text NOT NULL,
    asset text NOT NULL,
    network text NOT NULL,
    masked_destination text NOT NULL,
    -- Encrypted original destination and its supporting reference.
    details_enc bytea NOT NULL,
    review_state text NOT NULL DEFAULT 'pending_review' CHECK (review_state = 'pending_review'),
    created_by uuid NOT NULL REFERENCES staff_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (merchant_id,id),
    UNIQUE (target_id,version),
    UNIQUE (merchant_id,request_id),
    FOREIGN KEY (merchant_id,target_id,kind) REFERENCES payment_targets(merchant_id,id,kind),
    CHECK ((kind = 'bank' AND network = '') OR (kind = 'chain' AND network <> ''))
);
CREATE INDEX payment_target_latest ON payment_target_versions (merchant_id,target_id,version DESC);
