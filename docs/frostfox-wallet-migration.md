# FrostFox wallet migration protocol

This endpoint must be released together with FrostFox's repaired migration client.
The prior client cannot read the NewAPI response envelope and must not initiate transfers.

## Operational boundary

Deploy this version on **every** serving node with `WALLET_MIGRATION_ENABLED=true`, draining old instances normally during the rollout. Do not expose migration until no old/uncoordinated node remains. Keep the mode enabled after enrollment. Redis and `BATCH_UPDATE_ENABLED` may remain enabled; each migration pauses only the target account's new authenticated work.

The durable `wallet_migration_accounts` gate owns admission and drain. A request/refund, every wallet SQL write, and a detached quota batch retain activity until successful completion. The task poller retains activity for the entire snapshot/settlement iteration; transfer waits for a polling gap and no unfinished target tasks. SQL failures, failed retention, panic or process death cannot be declared complete by a timer. No automatic reset or TTL is safe.

On first freeze the target permanently uses direct database wallet quota reads/writes. Old Redis refill or quota mutations cannot restore spendable cached balance; other customers retain their existing batching/cache paths. This adds database admission/completion and accounting operations when coordinated mode is enabled. Throughput equivalence has not been benchmarked.

The transfer waits outside transactions for up to 20 seconds, then atomically verifies drain, exports quota, records the receipt and resumes the account. A busy attempt restores admission and is retryable with the same ID. Long streams/tasks need another attempt after completion. A crashed coordinator can leave a freeze; cancellation below clears only the matching freeze, never activity or money. A failed/crashed writer requires ledger reconciliation before its durable activity is repaired. Never reset counters merely because a process disappeared, and never disable coordination or downgrade code after enrollment.

## Endpoint

`POST /api/user/migration/wallet-transfer`, under existing **AdminAuth** and the global API rate limit. It deliberately does not use the shared login/critical-IP budget: FrostFox makes at least two calls per customer from one server IP, so that budget would lock unrelated customers out after ten migrations. The public registration is removed. Send the administrator account access token in `Authorization: Bearer <token>` plus its numeric ID in `New-Api-User`. A model API key is not an administrator access token. FrostFox administrators configure the full endpoint URL, administrator ID, access token and enabled flag under Settings > Legacy migration > NewAPI wallet migration. All administrators may save it. A database singleton stores the token encrypted with the existing application key ring; reads return only token presence and an empty token on save preserves the current value. The next migration reads the saved configuration without a restart; the former FrostFox environment variables are no longer the source. The client refuses redirects. Keep the endpoint attached to the same NewAPI database so user bindings and pending receipts retain their meaning.
Credentials authenticate the old account for each call; do not persist or log passwords.

Identify without changing quota:

```json
{"action":"inspect","username":"old-user","password":"<password>"}
```

Successful response:

```json
{"success":true,"message":"","data":{"user_id":123,"username":"old-user"}}
```

FrostFox validates veteran eligibility, establishes a unique binding using `user_id`, and commits a pending transfer before sending:

```json
{"action":"transfer","migration_id":"<durable-request-id>","user_id":123,"username":"old-user","password":"<password>"}
```

Successful response data contains `transfer_id`, `migration_id`, `user_id`, `amount` (integer quota), `quota_per_unit`, and `created_at`, inside the same `success/message/data` envelope. Zero balance produces a zero-amount receipt. Negative balances, disabled/admin users and identity mismatches cannot create a new transfer. An existing receipt is bound to its original user on every replay, including unique-key recovery.

Business errors retain NewAPI's HTTP 200 envelope with `success:false`. Notable errors are `wallet_migration_busy` and `wallet_migration_not_enabled`; HTTP success alone never proves transfer success.

Cancellation uses the same authenticated body as transfer with `action:"cancel"`. It only resumes the matching frozen ID; it does not refund an exported receipt, erase pending activity, or cancel another ID. Always retry the original operation to determine whether export committed.

## Recovery and compatibility

Retry an uncertain transfer using the **same migration_id and old user identity**. A committed debit returns the original immutable receipt. A genuinely new transfer after another deposit requires a new ID. FrostFox's pending row ID is its request ID; `transfer_id` is a separate NewAPI receipt ID.

FrostFox atomically commits the receipt and non-refundable additive credit under its existing account funds lock. A transport, response or local commit failure keeps the pending row, and the user can resubmit the same credentials to resume. No credential storage or credential-bearing background job is required.

Historical transfers created by the broken client may have no matching FrostFox pending record. Resolve those orphan receipts administratively using verified account ownership and ledger evidence; do not create guessed credit or silently bind ambiguous names. FrostFox preserves historical completed rows when adding its new binding/pending schema.

## Validation

`go test ./model ./middleware ./router ./service ./relay/channel/openai -count=1 -timeout 180s` runs related package suites. `TestWalletMigration*` covers ownership/replay, admission/freeze/resume, detached batches, deferred refund/poller lifetime, unfinished tasks, direct SQL and retention failures, stale profile writes, and the real admin route.

A local-only external harness ran two NewAPI processes against shared PostgreSQL and Redis: one node waited for the other's admitted debit; unrelated account admission continued; duplicate IDs returned one receipt; injected old Redis quota was ignored on both nodes. FrostFox tests separately verified admin headers/config validation, normal credit and lost-response recovery. SQLite and PostgreSQL were executed; MySQL-specific runtime verification and performance/load tests were not run. No production credentials or balances were used.
