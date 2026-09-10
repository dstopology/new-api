# FrostFox wallet migration protocol

This endpoint must be released together with FrostFox's repaired migration client.
The prior client cannot read the NewAPI response envelope and must not initiate transfers.

## Active-request snapshot policy (2026-09-10, implemented locally, not deployed)

The user explicitly authorizes ALL migration users to export while requests are active, accepting differences between the committed balance snapshot and eventual settlement. This supersedes the drain-before-export policy described in older release records. Transfer locks the target gate and wallet row, rechecks the owned receipt, and atomically zeros the committed quota, writes the immutable receipt and enables permanent direct-quota reads. It does not wait for activity, polling, tasks or detached batches and never clears their counters. Late debits may leave debt on the old site; late refunds and deposits stay there. Replaying a completed ID never exports those later funds, including when the old wallet is now negative.

This policy is not a per-user administrative bypass. Authentication, the inclusive USD 1 proof for a new transfer, receipt ownership, conflicting legacy freeze protection and FrostFox's unique pending/credit transaction remain mandatory. The production version is still the release recorded below until a new pinned release is completed.

## 2026-09-10 deployment evidence

NewAPI source `6329c7a9c84fad09e2899b1905ee584f52de279b` and FrostFox source `f4f380a97a0bb7a5564bd2fbbf0ad17a5cbd7a4b` were deployed together. The NewAPI image is `sha256:272cee55300a8dc8c0882c2f9ac950b4a74c9f91c0df3020d192b3255a01aeca`. The standard and candidate instances retained migration mode throughout natural connection drain; the final candidate was removed only after two zero-connection checks. A later documentation-only commit does not change this deployed source identity.

One production synthetic `inspect` verified the configured administrator authentication without a transfer, cancellation, account freeze or financial write. Credentials remained inside FrostFox Control. The live critical limit is enabled at 20 requests per 1200 seconds, shared by client IP; multi-user migrations can still exhaust that budget. Do not disable all critical throttling as a workaround.

Read-only ledger totals were 115 NewAPI receipts, 115 previously completed FrostFox rows and 3 pending rows; totals are not a receipt-by-receipt reconciliation and no real post-release transfer was executed by this release. Existing NewAPI no-available-channel 503 errors remained before and after cutover, despite healthy `/api/status`; this release did not repair channel configuration. See FrostFox's `doc/审计记录/REL-20260910-余额迁移修复与事故后加固发布.md` for source hashes, backups, verification commands and monitoring evidence.

## Migration rate-limit follow-up (2026-09-10)

The critical-bucket isolation was deployed as NewAPI `34a591d7e0886b5afe21dc7c7873b4ad0f7aa77b`, image `sha256:c120b5aa3be2d9b21a191bfc92e9fe4b226f8c1574516e6071133a0d9d511f8c`, without redeploying FrostFox. At 18:29:33 UTC, 21 consecutive synthetic identity `inspect` calls from the same FrostFox egress all reached normal administrator-authenticated identity verification without HTTP 429 or any financial operation. This supersedes the migration critical-bucket warning in the earlier deployment snapshot, not the remaining channel errors or global API capacity limit. Details: FrostFox's `doc/审计记录/REL-20260910-NewAPI迁移限流隔离修复.md`.

## Operational boundary

Migration calls are administrator-authenticated server traffic. They use the existing global API flood limiter, but do not participate in the login/reset `CriticalRateLimit` bucket. The global limiter still uses client IP (`GLOBAL_API_RATE_LIMIT_ENABLE`, `GLOBAL_API_RATE_LIMIT`, `GLOBAL_API_RATE_LIMIT_DURATION`; defaults enabled, 180 requests per 180 seconds). FrostFox separately limits public submissions by the authenticated FrostFox account to once per ten seconds. This removes the accidental 20-per-20-minute choke point without disabling authentication or making migration traffic unlimited. Critical limits on login, password reset and other existing routes are unchanged. The earlier deployment evidence above records the pre-fix policy; the subsequent rate-limit patch changes only this route registration.

Deploy this version on **every** serving node with `WALLET_MIGRATION_ENABLED=true`, draining old instances normally during the rollout. Keep the mode enabled after enrollment. Redis and `BATCH_UPDATE_ENABLED` may remain enabled. New snapshot transfers take only short database locks, without a persistent admission freeze or waiting for active requests.

The durable `wallet_migration_accounts` gate still owns accounting activity and legacy freeze compatibility. Requests, refunds, wallet writes, detached quota batches and task polling retain their activity until successful completion. Snapshot migration no longer waits for those counters or unfinished tasks. SQL failures, failed retention, panic or process death are not declared reconciled by exporting the snapshot. No automatic counter reset or TTL is introduced.

On the first committed export the target permanently uses direct database wallet quota reads/writes. Old Redis refill cannot restore spendable cached balance; detached batches still apply their existing deltas. Other customers retain their existing batching/cache paths. Full user updates must not restore stale quota; affiliate conversion uses the existing real row-lock helper and quota deltas, while invitation rewards update affiliate fields only. Throughput equivalence has not been benchmarked.

Transfer uses one transaction and no 100ms drain loop. The controller's 20-second context remains a database execution bound, not a requirement to finish all requests. An existing receipt is checked again under the target lock before fresh-balance verification. Receipt insertion failure rolls back the debit and enrollment. A legacy matching freeze is cleared atomically on success; another ID's freeze remains protected. Failed accounting work still needs independent reconciliation. Never reset counters merely because a process disappeared or disable coordination after enrollment.

## Endpoint

`POST /api/user/migration/wallet-transfer`, under existing **AdminAuth** and the global API rate limit, not the low-frequency critical bucket. The public registration is removed. Send the administrator account access token in `Authorization: Bearer <token>` plus its numeric ID in `New-Api-User`. A model API key is not an administrator access token. FrostFox administrators configure the full endpoint URL, administrator ID, access token and enabled flag under Settings > Legacy migration > NewAPI wallet migration. All administrators may save it. A database singleton stores the token encrypted with the existing application key ring; reads return only token presence and an empty token on save preserves the current value. The next migration reads the saved configuration without a restart; the former FrostFox environment variables are no longer the source. The client refuses redirects. Keep the endpoint attached to the same NewAPI database so user bindings and pending receipts retain their meaning.
The browser never sends the legacy password. The user copies the numeric legacy user ID from the NewAPI profile page and supplies that ID, the legacy username (or email alias), and an approximate current balance in site USD. FrostFox submits the ID as an integer and the balance as a decimal string so JSON floating-point conversion cannot change it. NewAPI resolves the username, requires the resolved user to equal the submitted `user_id`, and uses the existing `walletMigrationBalanceMatches` owner to compare `abs(expected_balance * QuotaPerUnit - quota) <= QuotaPerUnit` with exact decimal arithmetic. The inclusive tolerance is USD 1.00, not local currency, a percentage, or a rounded comparison. Unknown users, mismatched IDs, disabled/admin users, malformed values and out-of-tolerance estimates are rejected. Inspect and failure responses do not disclose the actual wallet balance.

On 2026-09-10 the user explicitly authorized this weaker balance proof, superseding exact equality. Actual USD 12.345678 accepts displayed 12.35 and both 11.345678 / 13.345678 boundaries, but rejects 11.345677 / 13.345679. For actual balances at most USD 1, an estimate of zero passes the balance part; usernames and IDs are not strong ownership credentials. Existing administrator authentication, eligibility, binding, cooldown and receipt ownership remain mandatory. The matching balance-proof and error-classification releases, administrator authentication probe and subsequent critical-bucket isolation are recorded above. Only the newer active-request snapshot policy remains local and undeployed.

Identify without changing quota:

```json
{"action":"inspect","migration_id":"","user_id":123,"username":"old-user","expected_balance":"12.35"}
```

Successful response:

```json
{"success":true,"message":"","data":{"user_id":123,"username":"old-user"}}
```

FrostFox validates veteran eligibility, establishes a unique binding using `user_id`, and commits a pending transfer before sending:

```json
{"action":"transfer","migration_id":"<durable-request-id>","user_id":123,"username":"old-user","expected_balance":"12.35"}
```

For a genuinely new transfer, NewAPI verifies the supplied balance with the same inclusive USD 1 tolerance under the wallet row lock and exports that committed integer quota, never the user's estimate or a promise of final settlement. Successful response data contains `transfer_id`, `migration_id`, `user_id`, `amount`, `quota_per_unit`, and `created_at` inside the same `success/message/data` envelope. Zero balance produces a zero-amount receipt. Negative balances, disabled/admin users and identity mismatches cannot create a new transfer. A negative balance caused by later settlement does not prevent recovery of an existing owned receipt.

If FrostFox already has a pending row, its inspect request includes that `migration_id`, the bound `user_id`, and the user's newly submitted ID. FrostFox rejects a submitted ID that differs from the binding before calling NewAPI. An existing owner-checked receipt is returned even if the wallet has already been zeroed; otherwise the newly supplied username, user ID, and current balance must still match. This preserves recovery after a lost response without weakening new-transfer verification. An existing receipt remains bound to its original user on every replay, including unique-key recovery.

Business errors retain NewAPI's HTTP 200 envelope with `success:false`. FrostFox maps only `wallet_migration_verification_failed`, `migration user changed` and `migration_id belongs to another user` to user verification failure. Busy, disabled mode, administrator authentication failures and unknown business errors map to service unavailable without exposing raw messages or automatic retries. HTTP success alone never proves transfer success.

Cancellation uses the same username/user-ID/balance proof and pending identity with `action:"cancel"`. It only resumes the matching frozen ID; it does not refund an exported receipt, erase pending activity, or cancel another ID. Always retry the original operation to determine whether export committed.

## Recovery and compatibility

Retry an uncertain transfer using the **same migration_id and old user identity**. A committed debit returns the original immutable receipt before checking the now-zero balance. A genuinely new transfer after another deposit requires a new ID and an estimate within USD 1 of the new current balance. FrostFox's pending row ID is its request ID; `transfer_id` is a separate NewAPI receipt ID.

FrostFox atomically commits the receipt and non-refundable additive credit under its existing account funds lock. A transport, response or local commit failure keeps the pending row, and the user can resubmit the user ID, username, and current displayed balance to resume. No password, balance proof, or credential-bearing background job is persisted.

Historical transfers created by the broken client may have no matching FrostFox pending record. Resolve those orphan receipts administratively using verified account ownership and ledger evidence; do not create guessed credit or silently bind ambiguous names. FrostFox preserves historical completed rows when adding its new binding/pending schema.

## Validation

`TestWalletMigrationRateLimitIsolation` exercises the real router with two target identities sharing one IP: migrations do not consume the login bucket, an exhausted login bucket does not block inspect, the global API flood budget still rejects excess calls, and wallet balances remain unchanged. Memory coverage always runs; Redis coverage uses the existing `TEST_REDIS_ADDR` convention, unique test keys and a disposable Redis instance. Never run this suite against production Redis. The administrator authorization regression remains mandatory.

`go test ./model ./middleware ./router ./service ./relay/channel/openai -count=1 -timeout 180s` runs related package suites. `TestWalletMigration*` covers inclusive USD tolerance boundaries, non-rounded comparison, zero/small balances, custom quota units, authoritative debit and replay after a top-up, ownership/replay, admission/freeze/resume, detached batches, deferred refund/poller lifetime, unfinished tasks, direct SQL and retention failures, stale profile writes, and the real admin route with a rounded display amount.

Historical drain-policy validation: a local-only external harness ran two NewAPI processes against shared PostgreSQL and Redis and waited for another node's debit. This is not validation of the new snapshot policy. Current snapshot tests cover active requests, late debit/refund, preserved poller/activity and unfinished tasks, detached batches, negative-wallet receipt recovery, concurrent same-ID replay, receipt-write rollback, and invitation races. Current PostgreSQL validation must be recorded separately; no historical result may substitute for it. No production credentials or balances are used in tests.
