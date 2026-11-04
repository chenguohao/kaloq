# Project working instructions

The user authorized syncing completed changes to the Singapore test server after each update.

- SSH target: `root@8.222.135.101`.
- Independent Demo: `https://v-2077.com/kaloq-demo/sandbox-demo`.
- Use `scripts/deploy-sandbox.sh` after relevant checks; verify the remote health endpoint and login flow.
- This Demo has its own service (`kaloq-demo`, loopback port 4101), database and login UI. Do not overwrite the existing `/kaloq/` deployment or other sites when updating the Demo.
- Keep vendor secrets/private keys under `secrets/` locally and `/opt/kaloq-demo/secrets/` remotely, outside Git and web roots. Never print credentials in logs or copy the credential PDF to hosting.
- This standing instruction authorizes deployment during work in this project; it does not request a scheduled background automation.

## Required development baseline

- Before implementation, read `v1.0/docs/development-baseline.md` and `v1.0/docs/README.md`. The baseline is mandatory for this sprint and later provider integrations; it is not a claim of implemented functionality.
- Preserve existing providers and in-flight orders when adding a provider. Bind execution steps to provider instances and original accounts; never reroute unknown outcomes or historical lookup using a new default.
- Design multi-provider identity, account bindings, funds locations, versioned snapshots and instance-scoped deduplication before finalizing business tables. Keep vendor-specific fields inside adapters/extensions.
- Use compatible, versioned database migrations. No destructive field changes, historical ledger rewrites or stop-and-reorganize data migrations for adding an existing capability. Verify old code/task compatibility and rollback before release.
- Include compatibility impact and validation evidence in data/execution/provider change reports. Follow the baseline's D1–D15 gates and two-instance isolation acceptance. Unresolved business parameters remain pending, not silently approved defaults.
- Documentation-only changes do not require runtime deployment. Preserve the deployment authorization and service isolation above for application/configuration updates.

## Formal implementation area

- New formal 1.0 implementation belongs in `v1.0/`; read its AGENTS.md and README.md first. Root `apps/`, `scripts/` and package manifests remain legacy test/Demo references. Keep dependencies, databases, migration ownership and build artifacts separate.

## Git commits (standing)

After each meaningful code or docs deliverable, create a clear local `git commit` on this repo before moving on. Split commits by logical unit when helpful. Never commit `secrets/` or credentials. Do not `git push` unless the user explicitly asks to push. After committing, update `HANDOFF.md` Evidence with the commit hash(es).

## Status handoff for Grok Bot
Keep `HANDOFF.md` at the repo root up to date so Grok Bot can watch progress without joining this session.

When you start a task, finish a meaningful step, hit a blocker, or finish the task: rewrite the whole `HANDOFF.md` with:
- Meta: Updated (Asia/Shanghai), Agent (Codex), State (`working` | `blocked` | `done`)
- Goal: one line
- Done / In progress / Blocked or questions / Next / Evidence (paths, commands, links)

Do not skip updates before long commands (tests, deploy, install). Keep secrets out of HANDOFF.md.
