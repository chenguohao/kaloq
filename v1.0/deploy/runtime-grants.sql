-- Executed by the deployment owner after migrations. Runtime cannot alter schema,
-- rewrite role/profile history, or modify/delete audit events.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO "kaloq-v1";
GRANT SELECT ON ALL TABLES IN SCHEMA public TO "kaloq-v1";
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "kaloq-v1";
GRANT INSERT, UPDATE, DELETE ON staff_sessions, recovery_codes, auth_rate_limits TO "kaloq-v1";
GRANT INSERT, UPDATE ON staff_users, merchants TO "kaloq-v1";
GRANT INSERT ON staff_role_assignments, audit_events, merchant_profile_versions TO "kaloq-v1";
GRANT INSERT, UPDATE ON staff_passkeys, staff_passkey_challenges TO "kaloq-v1";
GRANT DELETE ON staff_passkey_challenges TO "kaloq-v1";

-- Day 3. Runtime appends evidence and immutable order versions.
GRANT INSERT ON provider_instances, provider_bindings, payment_targets, payment_target_versions TO "kaloq-v1";
GRANT INSERT ON business_orders, order_versions, order_quote_versions, order_stages, order_execution_steps TO "kaloq-v1";
GRANT UPDATE(current_version) ON business_orders TO "kaloq-v1";
