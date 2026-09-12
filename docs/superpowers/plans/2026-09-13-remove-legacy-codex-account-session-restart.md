# Remove Legacy Codex Account-Switch Session Restart Implementation Plan

**Goal:** Remove the existing stop-before-switch and restart-after-switch subsystem while preserving safe, verified, credential-only Codex account switching.

**Stack:** Implement on `codex/remove-legacy-account-session-restart`, based on `fix/account-store-reconciliation` at `db3b704d2`. Open the PR against `fix/account-store-reconciliation`; retarget it to `main` after the parent PR merges.

**Delivery:** Land the removal as one buildable commit:

```text
refactor(codex): remove legacy account-switch session restart flow
```

The follow-up idle-session reconnect design and implementation are intentionally out of scope. They will receive a separate plan and stacked commit/PR after this removal is reviewed.

## Desired End State

Account switching has one responsibility:

```text
admit request
  -> checkpoint current credential
  -> activate target credential
  -> verify target identity
  -> complete

activation or verification failure
  -> restore source credential
  -> verify managed source when applicable
  -> fail safely
```

The switch must not enumerate, fence, interrupt, stop, resume, or report on worker or reviewer sessions. Running sessions remain untouched. New controller launches use the newly active account.

The daemon-wide Codex credential-mutation gate remains. It prevents a new controller from being admitted while the device-global credential is being replaced, but it does not stop an already-running controller.

## Non-Goals

- Do not implement idle-session discovery or reconnection.
- Do not introduce `reconnectIdleSessions` yet.
- Do not reuse or modify the interface-transition implementation.
- Do not change TUI resume, Chat resume, controller-generation, or persistent-host behavior.
- Do not change account login, device reconciliation, deduplication, capacity, or reset-credit behavior from the parent PR.
- Do not modify migrations `0124`, `0140`, `0141`, or any other merged migration.
- Do not remove shared session-operation, terminal-input, Chat handoff, reviewer, or lifecycle facilities that are used outside Codex account switching.

## Compatibility Decisions

The SQLite schema contains historical restart artifacts:

- `codex_account_switches.restart_running_sessions`
- legacy switch phase values such as `stopping_sessions`, `sessions_stopped`, and `restarting_sessions`
- `codex_account_switch_sessions` and its CDC triggers

These remain in immutable migration history. The active application must stop interpreting, writing as enabled, or exposing them. A generated select may still scan the historical restart column to preserve a shared sqlc row shape, but the store must ignore it. New switch inserts should explicitly store `restart_running_sessions = FALSE` so the historical `DEFAULT TRUE` cannot describe new behavior incorrectly.

Generated sqlc model declarations that exist only because the historical table remains are accepted generated artifacts; do not hand-edit them. Delete the active session-row queries and store methods, then regenerate sqlc.

An upgrade can encounter one nonterminal switch created by the previous build. Keep one narrow compatibility settlement at the account-switch recovery boundary:

- `requested` continues as a credential-only switch without using its old restart flag.
- `stopping_sessions` or `sessions_stopped` advances to `checkpointing_source` and finishes the requested credential switch without performing more session work. The source credential is still authoritative at these pre-activation phases; any sessions already stopped remain available through the ordinary Resume action.
- `restarting_sessions` verifies the target credential. If the target is active, complete the switch without restarting more sessions. Otherwise enter the existing credential rollback/recovery path.
- Credential phases (`checkpointing_source`, `activating_target`, `verifying_target`, `rollback_required`, and `recovery_required`) continue through credential-only recovery.

Legacy phase recognition must be private compatibility code, not an active transition emitted by new switches or a session-restart path exposed to clients.

## Task 1: Lock the Credential-Only Contract in Tests

Modify:

- `backend/internal/session_manager/codex_account_management_test.go`
- `backend/internal/httpd/controllers/codex_accounts_test.go`
- `backend/internal/storage/sqlite/store/codex_account_management_store_test.go`
- `backend/internal/httpd/apispec/specgen/build_test.go`
- `frontend/src/renderer/components/settings/CodexAccountsSection.test.tsx`
- `frontend/src/renderer/hooks/codex-accounts-state.test.ts`
- `frontend/src/renderer/components/SessionView.test.tsx`

Add or rewrite focused tests proving:

- A successful switch performs checkpoint, activation, target verification, and completion without listing or mutating sessions.
- A target verification failure restores the source credential and never touches sessions.
- Idempotency depends on target account and expected revision, not a restart preference.
- New persisted switches record the historical restart column as false.
- Startup/manual recovery folds each legacy session phase into the compatibility behavior above without invoking session stop/resume operations.
- The HTTP request and response no longer accept or expose restart policy or session progress.
- The settings confirmation contains no restart toggle.
- Session input is not disabled merely because a credential-only account switch is active.
- Frontend recovery remains credential recovery only and contains no “reconnect sessions” presentation.

Run the focused tests and confirm that failures identify every old behavior that subsequent tasks remove.

## Task 2: Simplify the Domain and Port Contracts

Modify:

- `backend/internal/domain/codex_account_switch.go`
- `backend/internal/ports/codex_account_management.go`

Remove from the active domain:

- `CodexAccountSwitchSession`
- `CodexAccountSwitch.RestartRunningSessions`
- `CodexAccountSwitch.Sessions`
- active transition constants for stopping, stopped, and restarting sessions
- restart-related completion wording

Remove from ports:

- `CodexAccountSwitchConfig.RestartRunningSessions`
- `ListCodexAccountSwitchSessions`
- `UpdateCodexAccountSwitchSession`

Keep the credential phases, source-kind model, idempotency data, credential commit timestamp, terminal-state behavior, and recovery capability.

If legacy phase strings are needed for upgrade settlement, define them privately beside the recovery code rather than advertising them as active domain states.

## Task 3: Remove Session Orchestration from the Coordinator

Modify:

- `backend/internal/session_manager/codex_account_switch.go`
- `backend/internal/session_manager/codex_account_management_test.go`
- `backend/internal/session_manager/manager.go`
- `backend/internal/session_manager/session_input.go`
- `backend/internal/review/review.go`
- `backend/internal/service/review/review.go`

Change switch admission:

- Build the idempotency fingerprint from target account and expected revision only.
- Preserve account-store readiness, immediate device reconciliation, global switch admission, credential mutation, source/revision validation, target-account verification, and login exclusion.
- Create the durable switch immediately after credential validation.
- Start the credential worker without a session admission object.

Delete the account-switch-specific session machinery:

- pre-switch session/reviewer snapshot construction
- TUI running/fresh-restart classifiers used only by account switching
- worker/controller identity validation used only by account switching
- reserved restart generations and target-owner adoption
- Chat controller reconstruction used only by account-switch recovery
- per-session operation acquisition/reclamation and retention
- sequential stop and restart loops
- reviewer suspend/resume work
- account-switch-only reviewer snapshot/suspend/restore adapters
- Chat interrupt arm/prepare/abort callbacks
- terminal input freezing
- per-session progress persistence and compare-and-swap helpers
- session-list loading into switch responses

Simplify the dispatcher to credential phases only. A completed credential switch must not wait on session work, and a session condition must never trigger credential rollback.

Retain:

- the daemon-wide exclusive Codex mutation/admission fence
- checkpoint/activate/verify/rollback/cleanup behavior
- durable phase compare-and-swap and idempotent read-back
- manual and startup credential recovery
- the private legacy-phase settlement described above

## Task 4: Remove Active Storage Access to Restart State

Modify source files:

- `backend/internal/storage/sqlite/queries/codex_account_management.sql`
- `backend/internal/storage/sqlite/store/codex_account_management_store.go`
- `backend/internal/storage/sqlite/store/codex_account_management_store_test.go`

Regenerate:

- `backend/internal/storage/sqlite/gen/codex_account_management.sql.go`
- `backend/internal/storage/sqlite/gen/models.go`

Changes:

- Remove insert/list/update queries for `codex_account_switch_sessions`.
- Stop projecting `restart_running_sessions` into the domain model.
- Insert `restart_running_sessions` as a literal false compatibility value.
- Remove session-row insertion from switch creation.
- Remove session-row store methods and mapping helpers.
- Continue reading and updating all credential-switch fields required for recovery.

Do not add manual `change_log` writes. The historical triggers remain inert because new switches create no session rows.

Run:

```bash
npm run sqlc
cd backend && go test ./internal/storage/sqlite/... ./internal/session_manager/...
```

## Task 5: Remove Restart State from the HTTP API

Modify source files:

- `backend/internal/httpd/controllers/dto.go`
- `backend/internal/httpd/controllers/codex_accounts.go`
- `backend/internal/httpd/controllers/codex_accounts_dto.go`
- `backend/internal/httpd/controllers/codex_accounts_test.go`
- `backend/internal/httpd/apispec/specgen/build.go`
- `backend/internal/httpd/apispec/specgen/build_test.go`

Regenerate:

- `backend/internal/httpd/apispec/openapi.yaml`
- `frontend/src/api/schema.ts`

Remove:

- `restartRunningSessions` from the start request
- `restartRunningSessions` from the switch response
- the per-session switch response schema and `sessions` response field
- active API enum exposure of session stop/restart phases
- session-error redaction code that no longer has an input

Keep the recovery endpoint because credential activation and rollback can still require retry.

Run:

```bash
npm run api
cd backend && go test ./internal/httpd/...
```

## Task 6: Simplify the Settings and Session UI

Modify:

- `frontend/src/renderer/components/settings/CodexAccountsSection.tsx`
- `frontend/src/renderer/hooks/useCodexAccountActions.ts`
- `frontend/src/renderer/hooks/useCodexAccountsQuery.ts`
- `frontend/src/renderer/hooks/codex-accounts-state.ts`
- `frontend/src/renderer/components/SessionView.tsx`
- their corresponding tests
- `frontend/src/renderer/i18n/en.json`
- `frontend/src/renderer/i18n/de.json`
- `frontend/src/renderer/i18n/es.json`
- `frontend/src/renderer/i18n/fr.json`
- `frontend/src/renderer/i18n/ja.json`
- `frontend/src/renderer/i18n/ko.json`
- `frontend/src/renderer/i18n/pt-BR.json`
- `frontend/src/renderer/i18n/zh-CN.json`

Changes:

- Remove the restart preference from pending switch state and mutation arguments.
- Remove the restart toggle, tooltip, and on/off explanatory copy.
- Keep one ordinary switch confirmation.
- Remove session-stop/restart phase presentation and failure-code mappings.
- Remove session-specific recovery classification and the “Reconnect sessions” action label.
- Keep generic account switch progress, completion, restored-source, failure, and credential recovery presentation.
- Remove account-switch restart state from SessionView input-disable conditions.
- Do not add idle-session messaging in this removal PR.

The end-user surface after this commit should communicate only account switching, not session management.

## Task 7: Update Documentation Without Designing the Replacement

Modify:

- `docs/STATUS.md`
- `docs/research/2026-08-31-codex-global-account-management-architecture.md`
- `frontend/src/landing/content/docs/plugins/agents/codex.mdx`
- `docs/README.md` only if its linked description becomes inaccurate

Document the temporary but complete product contract:

- account switching changes the device-global credential after verification
- running sessions are not interrupted or restarted
- new controllers use the active account
- users can manually resume/restart an existing session when they want it relaunched

Do not document the proposed idle reconnect flow as shipped behavior. Its separate implementation plan can link back to this removal plan.

## Task 8: Full Verification and Single Commit

Run focused checks first, then repository-level checks:

```bash
cd backend && go test ./internal/session_manager/... ./internal/service/agent/... ./internal/storage/sqlite/... ./internal/httpd/...
npm run frontend:typecheck
cd frontend && npm test -- --run src/renderer/components/settings/CodexAccountsSection.test.tsx src/renderer/hooks/codex-accounts-state.test.ts src/renderer/components/SessionView.test.tsx
npm run lint
```

Also run `git diff --check` and confirm:

- no old migration was edited
- generated sqlc and OpenAPI artifacts match their sources
- no account-switch code enumerates or mutates sessions/reviewers
- no public request/response exposes restart state
- parent-branch account reconciliation behavior remains covered
- the only legacy restart references outside immutable migrations/generated schema are the narrow upgrade-settlement constants/tests

Commit all implementation changes together:

```text
refactor(codex): remove legacy account-switch session restart flow
```

## Acceptance Criteria

- Switching a verified account succeeds while existing TUI and Chat controllers continue untouched.
- Account activation is complete before the operation reports success.
- Activation or verification failure restores or safely recovers the credential without involving sessions.
- No session/reviewer snapshot, stop, restart, generation reservation, or progress row is created by a new switch.
- No account rollback can be caused by a session condition.
- The API and settings UI contain no legacy restart option, phase, session result, or recovery action.
- An old nonterminal switch can settle after upgrade without resuming the deleted restart workflow.
- The branch is ready for a second stacked plan implementing post-switch parallel reconnection of only currently idle sessions.
