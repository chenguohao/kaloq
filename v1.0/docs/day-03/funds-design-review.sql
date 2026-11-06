-- D3 model review for D4–D6. Run with psql -v ON_ERROR_STOP=1 on an isolated
-- rehearsal database after migrations 001–004. All objects roll back on exit.
BEGIN;
DO $$
BEGIN
  IF current_database() NOT LIKE 'kaloq_v1_%' OR
     current_database() !~ '(_test|_rehearsal)$' THEN
    RAISE EXCEPTION 'dedicated test or rehearsal database required';
  END IF;
END $$;

CREATE SCHEMA d3_review;
SET LOCAL search_path = d3_review, public;
CREATE TABLE asset_definitions (
  id uuid PRIMARY KEY,
  code text NOT NULL, network text NOT NULL, contract_ref text NOT NULL,
  scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
  UNIQUE (code, network, contract_ref)
);
CREATE TABLE fund_locations (
  id uuid PRIMARY KEY, instance_id uuid NOT NULL REFERENCES public.provider_instances,
  -- A platform pool may have its own account. An observed merchant VA needs an
  -- independent qualification decision before it may become an execution source.
  account_scope text NOT NULL, account_fingerprint bytea NOT NULL,
  asset_id uuid NOT NULL REFERENCES asset_definitions, evidence_ref uuid NOT NULL,
  UNIQUE (instance_id, account_scope, account_fingerprint, asset_id),
  UNIQUE (id, instance_id, asset_id), UNIQUE (id, asset_id)
);
CREATE TABLE positions (
  id uuid PRIMARY KEY, merchant_id uuid REFERENCES public.merchants,
  owner_kind text NOT NULL CHECK (owner_kind IN ('merchant','platform','unclaimed')),
  location_id uuid NOT NULL, asset_id uuid NOT NULL,
  bucket text NOT NULL CHECK (bucket IN ('available','reserved','in_transit')),
  minor numeric(38,0) NOT NULL CHECK (minor >= 0),
  CHECK ((owner_kind = 'merchant') = (merchant_id IS NOT NULL)),
  FOREIGN KEY (location_id, asset_id) REFERENCES fund_locations(id, asset_id),
  UNIQUE NULLS NOT DISTINCT (owner_kind, merchant_id, location_id, asset_id, bucket)
);
CREATE TABLE policy_versions (
  policy_id uuid NOT NULL, version bigint NOT NULL CHECK (version > 0),
  rules jsonb NOT NULL, authorized_by uuid NOT NULL REFERENCES public.staff_users,
  PRIMARY KEY (policy_id, version)
);
CREATE TABLE approvals (
  id uuid PRIMARY KEY, merchant_id uuid NOT NULL, order_id uuid NOT NULL, version bigint NOT NULL,
  policy_id uuid NOT NULL, policy_version bigint NOT NULL,
  canonical_version smallint NOT NULL, encrypted_snapshot bytea NOT NULL,
  digest bytea NOT NULL CHECK (octet_length(digest)=32),
  FOREIGN KEY (merchant_id, order_id, version) REFERENCES public.order_versions(merchant_id,order_id,version),
  FOREIGN KEY (policy_id, policy_version) REFERENCES policy_versions,
  UNIQUE (id, merchant_id, order_id, version)
);
-- Every dispatchable request must prove its snapshot and source binding. A
-- transfer that credits a second location adds a separate observed credit.
CREATE TABLE dispatches (
  id uuid PRIMARY KEY, merchant_id uuid NOT NULL, order_id uuid NOT NULL, version bigint NOT NULL,
  approval_id uuid NOT NULL, ordinal int NOT NULL, action text NOT NULL,
  instance_id uuid NOT NULL, source_id uuid NOT NULL, asset_id uuid NOT NULL,
  target_version_id uuid, request_id uuid NOT NULL,
  source_minor numeric(38,0) NOT NULL CHECK (source_minor > 0),
  FOREIGN KEY (approval_id,merchant_id,order_id,version) REFERENCES approvals(id,merchant_id,order_id,version),
  FOREIGN KEY (source_id,instance_id,asset_id) REFERENCES fund_locations(id,instance_id,asset_id),
  FOREIGN KEY (merchant_id,target_version_id) REFERENCES public.payment_target_versions(merchant_id,id),
  UNIQUE (instance_id, request_id), UNIQUE (approval_id, ordinal)
);
CREATE TABLE journal_headers (
  id uuid PRIMARY KEY, posting_key text NOT NULL UNIQUE,
  step_id uuid REFERENCES dispatches, reversal_of uuid REFERENCES journal_headers
);
CREATE TABLE journal_entries (
  journal_id uuid NOT NULL REFERENCES journal_headers, ordinal int NOT NULL,
  asset_id uuid NOT NULL REFERENCES asset_definitions, account_id uuid NOT NULL,
  delta_minor numeric(38,0) NOT NULL CHECK (delta_minor <> 0), PRIMARY KEY (journal_id,ordinal)
);
CREATE TABLE inbox (
  instance_id uuid NOT NULL REFERENCES public.provider_instances,
  account_scope text NOT NULL, event_id text NOT NULL, schema_version int NOT NULL,
  payload_digest bytea NOT NULL, PRIMARY KEY (instance_id,account_scope,event_id)
);
CREATE TABLE posting_claims (
  instance_id uuid NOT NULL REFERENCES public.provider_instances, account_scope text NOT NULL,
  operation_id text NOT NULL, posting_kind text NOT NULL, asset_id uuid NOT NULL REFERENCES asset_definitions,
  event_index int NOT NULL, journal_id uuid NOT NULL REFERENCES journal_headers,
  PRIMARY KEY (instance_id,account_scope,operation_id,posting_kind,asset_id,event_index)
);
ROLLBACK;
