-- Day 3 records instructions and planned work. No rows here dispatch funds.
CREATE TABLE business_orders (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL REFERENCES merchants(id),
    order_number text NOT NULL UNIQUE,
    direction text NOT NULL CHECK (direction IN ('fiat_to_crypto','crypto_to_fiat')),
    current_version bigint NOT NULL CHECK (current_version > 0),
    created_by uuid NOT NULL REFERENCES staff_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (merchant_id,id)
);
CREATE INDEX business_orders_merchant_feed ON business_orders(merchant_id,created_at DESC,id DESC);

CREATE TABLE order_versions (
    order_id uuid NOT NULL,
    merchant_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    actor_id uuid NOT NULL REFERENCES staff_users(id),
    request_id uuid NOT NULL,
    request_digest bytea NOT NULL CHECK (octet_length(request_digest)=32),
    profile_version bigint NOT NULL,
    target_version_id uuid,
    channel_binding_id uuid,
    instruction_enc bytea NOT NULL,
    source_minor numeric(38,0) NOT NULL CHECK(source_minor > 0),
    source_asset text NOT NULL,
    source_scale smallint NOT NULL CHECK(source_scale BETWEEN 0 AND 18),
    destination_asset text NOT NULL,
    hold_reason text NOT NULL,
    next_action text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(order_id,version),
    UNIQUE(merchant_id,request_id),
    UNIQUE(merchant_id,order_id,version),
    FOREIGN KEY(merchant_id,order_id) REFERENCES business_orders(merchant_id,id),
    FOREIGN KEY(merchant_id,profile_version) REFERENCES merchant_profile_versions(merchant_id,version),
    FOREIGN KEY(merchant_id,target_version_id) REFERENCES payment_target_versions(merchant_id,id),
    FOREIGN KEY(merchant_id,channel_binding_id) REFERENCES provider_bindings(merchant_id,id)
);
ALTER TABLE business_orders ADD CONSTRAINT business_orders_current_snapshot
    FOREIGN KEY(id,current_version) REFERENCES order_versions(order_id,version) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE order_quote_versions (
    order_id uuid NOT NULL,
    version bigint NOT NULL,
    rate numeric NOT NULL CHECK(rate > 0),
    destination_minor numeric(38,0) NOT NULL CHECK(destination_minor > 0),
    destination_scale smallint NOT NULL CHECK(destination_scale BETWEEN 0 AND 18),
    expires_at timestamptz NOT NULL,
    evidence_enc bytea NOT NULL,
    PRIMARY KEY(order_id,version),
    FOREIGN KEY(order_id,version) REFERENCES order_versions(order_id,version)
);
CREATE TABLE order_stages (
    order_id uuid NOT NULL,
    version bigint NOT NULL,
    ordinal smallint NOT NULL CHECK(ordinal>0),
    code text NOT NULL,
    label text NOT NULL,
    status text NOT NULL CHECK(status IN ('recorded','blocked','pending')),
    reason text NOT NULL DEFAULT '',
    PRIMARY KEY(order_id,version,code),
    UNIQUE(order_id,version,ordinal),
    FOREIGN KEY(order_id,version) REFERENCES order_versions(order_id,version)
);
CREATE TABLE order_execution_steps (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL,
    order_id uuid NOT NULL,
    version bigint NOT NULL,
    ordinal smallint NOT NULL CHECK(ordinal>0),
    stage_code text NOT NULL,
    action text NOT NULL,
    label text NOT NULL,
    status text NOT NULL CHECK(status='planned'),
    instance_id uuid,
    binding_id uuid,
    CHECK ((instance_id IS NULL) = (binding_id IS NULL)),
    UNIQUE(order_id,version,ordinal),
    FOREIGN KEY(merchant_id,order_id,version) REFERENCES order_versions(merchant_id,order_id,version),
    FOREIGN KEY(order_id,version,stage_code) REFERENCES order_stages(order_id,version,code),
    FOREIGN KEY(merchant_id,binding_id,instance_id) REFERENCES provider_bindings(merchant_id,id,instance_id)
);
-- Revisions create new facts rather than overwriting instructions or quotes.
CREATE TRIGGER order_versions_immutable BEFORE UPDATE OR DELETE ON order_versions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
CREATE TRIGGER order_quote_versions_immutable BEFORE UPDATE OR DELETE ON order_quote_versions FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();
