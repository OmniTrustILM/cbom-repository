# Change feed for `GET /v1/bom`: design decision

cbom-repository#138, deliverable 2. Written 2026-09-02. Companion file with the drafted follow-up issues: [2026-09-02-change-feed-follow-up-issues.md](./2026-09-02-change-feed-follow-up-issues.md).

## 1. Status and scope

| Field | Value |
|---|---|
| Status | Proposed, for Tech Lead review |
| Decision requested | Approve one change-feed option (section 9), the #26 and #27 reconciliation (section 10) and the stats backfill decision (section 11) |
| Decided by | ____ (Tech Lead) |
| Date | ____ |
| Author and owner | Lane B, task T8 of epic ilm#299, issue cbom-repository#138 |
| Issues | cbom-repository#138 (this decision), #26 (webhook delivery), #27 (stats metadata and search), #121 (MinIO image discontinued), ilm#299 (epic and document-store constraint), core#2073 (sync and deletion lifecycle), core#2072 (asset extraction, closed) |

This document decides how a consumer discovers new documents in the repository. It changes no code. Deliverable 1 of #138 (the `limit` parameter, warning entries, nested counting, the OpenAPI corrections) is on branch `feat/138-search-hardening-change-feed`, pending merge; this document describes it as specified there and builds on it.

Abbreviations, expanded once: S3 (Amazon Simple Storage Service, and the API that MinIO and other stores implement), CBOM (Cryptography Bill of Materials), HPA (Kubernetes Horizontal Pod Autoscaler), HMAC (hash-based message authentication code), CAS (compare-and-swap), ETag (entity tag), NTP (Network Time Protocol), DTO (data transfer object), SDK (software development kit), PQC (post-quantum cryptography), URN (Uniform Resource Name), TLS (Transport Layer Security), SNS and SQS (Amazon Simple Notification Service and Simple Queue Service), RGW (Ceph RADOS Gateway), PR (pull request).

## 2. Context

### 2.1 How the feed works today

Facts read from this repository's code on 2026-09-02:

- `store.Search` (`internal/store/store.go`) lists the whole bucket with `ListObjectsV2` without a prefix and keeps every object whose `LastModified` is strictly later than `time.Unix(after, 0)`. The compare runs at nanosecond precision. Keys arrive in the store's listing order, which is UTF-8 binary key order.
- `service.Search` (`internal/service/service.go`) issues one `HeadObject` per kept key, splits the key at its last `-` into `serialNumber` and `version`, reads the `crypto-stats` user metadata and emits `{serialNumber, version, created_at, cryptoStats}`. `created_at` is the HEAD `LastModified` formatted as RFC 3339 at second precision.
- The handler (`internal/http/handlers.go`) requires `after` and rejects any value that is not a non-negative integer.
- Before deliverable 1, an object without `crypto-stats` metadata was dropped silently, and one object with unparseable `crypto-stats` failed the whole call with 500.
- Uploads (`internal/service/upload.go`, `internal/store/connect.go`) buffer the entire request body, validate it, and only then write the object `<serialNumber>-<version>` (plus `<serialNumber>-original` when the client sent no serial number) through the aws-sdk-go-v2 transfer manager with default options. The transfer manager switches to a multipart upload at 16 MiB (`defaultMultipartUploadThreshold` in `transfermanager v0.3.5`), and the body cap `APP_HTTP_MAX_BODY_SIZE` defaults to 20 MiB, so multipart uploads are possible but rare. Because the body is buffered first, the store-side upload duration is the in-cluster transfer from the pod to the store, not the client's transfer. Overwrites are not intended (two of the three upload paths check `KeyExists` before writing; concurrent uploads of the same serial number and version can still race) and the API has no delete operation.
- Deployment (`deploy/charts/cbom-repository/values.yaml`): `replicaCount: 1`; `autoscaling.enabled: false` with `maxReplicas: 100` when enabled; the service has no authentication of its own; the embedded MinIO (`RELEASE.2025-09-07T16-13-09Z`) is disabled by default and marked discontinued (#121). The service keeps no state outside the bucket.

### 2.2 Who consumes it

Facts read from Core's source on 2026-09-02 (`CbomSyncTask`, `CbomServiceImpl.sync()`, `CbomRepositoryClient`, `BomEntryDto`, `CbomAssetExtractor`, `DocumentScope.walk`):

- `CbomSyncTask` runs hourly: its `CRON_EXPRESSION` constant is `0 0 * ? * *` (`src/main/java/com/otilm/core/tasks/CbomSyncTask.java`, read 2026-09-02). `CbomServiceImpl.sync()` calls `GET /api/v1/bom?after=<ts>` with `ts` equal to the start time of the last successful run minus 60 s, on Core's clock, or 0 on the first run.
- Core deduplicates on `(serialNumber, version)` with `existsBySerialNumberAndVersion` plus a unique constraint before it fetches anything. An entry that throws (null dereference, validation) is counted as skipped and the loop continues; the run still counts as successful.
- For every new entry Core fetches the document (`GET /api/v1/bom/{urn}?version=`) and stores header fields plus the entry's `cryptoStats` counts on the `cbom` table.
- Core's `BomEntryDto` declares a field `timestamp`. The repository emits `created_at`, so that field is always null and unused; Core reads the document's own `metadata.timestamp`. The wire does not change; deliverable 1 corrects the OpenAPI document to `created_at`.
- core#2072 (closed) walks the `components` tree iteratively, depth-first, in document order, to a depth of at most 1000 (`CbomAssetExtractor`, `DocumentScope.walk`, `MAX_DEPTH`). core#2073 (open) is the ingest that will parse every component of every CBOM; its text reads "Discovery via the existing after-watermark search; 'original' pseudo-version filtered". The consolidated epic overview (revision 4.4, 2026-09-02, section 8, risk register) lists "repository feed gaps → tombstone-aware reconciliation + T8", where T8 is this issue.

The epic's volume target of "millions of distinct assets" (consolidated epic overview, revision 4.4, section 4) refers to assets inside Core, not to documents in the repository. The only measured corpus, `cbom-corpus-2026-08-31.tar.gz` (consolidated epic overview, revision 4.4, 2026-09-02, section 10 "Validation corpus (local)"), holds 180 deduplicated real-world CBOMs with 4,815 assets (4,819 routed, 2,133 identities), about 27 assets per document. The repository population is therefore expected in the thousands of documents; Core holds the millions of rows.

### 2.3 What deliverable 1 changes

Everything in this subsection is on the #138 branch, pending merge, and is described as specified there.

- `GET /v1/bom` accepts an optional `limit` (1 to 1000, inclusive; values above 1000 are rejected, not clamped). Without `limit` the call is byte-compatible with the legacy behaviour.
- In paged mode the boundary is evaluated at second granularity (`LastModified.Unix() > after`), candidates are sorted by `(LastModified asc, key asc)`, and the limit is soft: a page never splits a second, so it may exceed `limit` by the rest of the boundary second.
- Objects without `crypto-stats` metadata appear with `cryptoStats: null` and `warnings: ["crypto-stats-missing"]`; unparseable metadata gives `cryptoStats: null` and `warnings: ["crypto-stats-invalid"]`. One bad object no longer fails the call. `GET /v1/bom/{urn}/versions` behaves the same way.
- `CalculateCryptoStats` counts nested components (depth bound 1000, `components` tree only, matching Core's walker). New uploads carry the S3 user metadata `crypto-stats-version: 2`; its absence marks the legacy shallow count. The version is not on the JSON wire.
- OpenAPI: `created_at` replaces `timestamp`, version fields accept the string `original`, `warnings` and nullable `cryptoStats` are documented, `info.version` is `0.2.0`.
- The documented client protocol (OpenAPI and README):

```
run_start := now()                       // consumer clock
after := watermark                       // from the previous run
for {
    page := GET /v1/bom?after={after}&limit=N
    process page                         // dedup by (serialNumber, version) is the consumer's job
    if len(page) < N { break }           // fewer than N: last page
    after = unix(page[len(page)-1].created_at)
}
watermark = unix(run_start) - overlap    // overlap >= clock skew + longest upload duration + 1 s; Core uses 60 s
```

Deliverable 1 ships no cursor and no sequence number. This document is the decision on those.

## 3. Problem statement

### 3.1 Gap classes

| Class | Mechanism (one line) | After deliverable 1 | Who closes the rest |
|---|---|---|---|
| (i) Same-second boundary | `after` has second resolution and the filter is strict, so objects stamped in the same second as the last returned entry but not on that page would be skipped. | Closed within a run by "never split a second"; the page carries the whole boundary second. | Option (a) makes the boundary exact by key, which allows a hard limit. |
| (ii) Open second | An object becomes listable after the page containing its second was built: either the upload completes later in that second, or it is a multipart upload, whose `LastModified` is the initiation time, so it appears with a timestamp older than objects listed before it. | Missed by the current run; caught by the next run because the watermark is `run_start - overlap`, not the last `created_at`. | Stays a protocol rule in every option; the overlap must exceed the longest initiation-to-completion interval plus skew. |
| (iii) Clock skew | Store nodes stamp `LastModified` with their own clocks, so visibility order and timestamp order differ; Core's watermark uses Core's clock but is compared against store timestamps. | Covered only while the total skew fits inside the overlap. | Overlap sizing and NTP on Core and the store; the open question in section 13 asks whether Core should derive the watermark from feed timestamps instead. |
| (iv) Missing or corrupt metadata | Objects without or with unparseable `crypto-stats` were invisible or broke the call. | Closed: visible with `warnings`. | Core must tolerate `cryptoStats: null` (follow-up issue 2). Baseline: today the repository drops such objects, so Core never sees them; if Core dereferences the field after deliverable 1, the entry fails, is counted as skipped, and the moving watermark never shows it again, which is the same end state as today, not a regression. For invalid metadata deliverable 1 is a strict improvement, because one bad object no longer fails the whole feed with 500. |
| (v) Deletions | An `after` feed lists what exists; a deleted object leaves no trace. The API has no delete, but an operator can delete in the bucket. | Not addressed. | Core's tombstone-aware reconciliation (core#2073). Options (a) and (b) as drafted carry no delete events; see trigger T7 in section 9. |
| (vi) Cost amplification | Every poll lists the whole bucket (`ceil(N / 1000)` LIST requests) regardless of how many objects changed, then issues one HEAD per candidate; paged mode repeats the full LIST for every page. | Bounded per call by `limit`, multiplied across pages. | Options (b) and (c) address it; section 3.2 quantifies it. |

### 3.2 Cost of the polled feed

Unit prices, approximately, per the AWS pricing page as read on 2026-09-02 (S3 Standard, us-east-1): LIST (billed like PUT, COPY, POST) $0.005 per 1,000 requests, so $0.000005 per LIST page of up to 1,000 keys; HEAD (billed with GET) $0.0004 per 1,000 requests, so $0.0000004 per HEAD. Conditional requests cost nothing extra; failed requests are still billed. Every figure below is derived from these two unit prices and can be recomputed from them. A month is taken as 30 days: 720 hourly polls or 43,200 one-minute polls. The steady-state rows assume 50 new documents per poll; the LIST term does not depend on that assumption.

Steady state, one page per poll:

| Documents N | LIST per poll | HEAD per poll | Cost per poll | Per month, hourly | Per month, every minute |
|---|---|---|---|---|---|
| 1,000 | 1 | 50 | $0.000025 | $0.018 | $1.08 |
| 10,000 | 10 | 50 | $0.00007 | $0.050 | $3.02 |
| 100,000 | 100 | 50 | $0.00052 | $0.37 | $22.46 |

Cold run (first run, `after=0`, or a watermark older than every object); paged with `limit=1000` versus the unpaged legacy call:

| Documents N | Pages | LIST paged | LIST unpaged | HEAD | Cost paged | Cost unpaged |
|---|---|---|---|---|---|---|
| 1,000 | 1 | 1 | 1 | 1,000 | $0.000405 | $0.000405 |
| 10,000 | 10 | 100 | 10 | 10,000 | $0.0045 | $0.00405 |
| 100,000 | 100 | 10,000 | 100 | 100,000 | $0.09 | $0.0405 |

Reading of the tables:

- In dollars the feed is negligible at every size considered, even at one poll per minute. Money is not a decision driver here.
- LIST dominates above about 4,000 documents (five LIST pages per poll) under the table's assumption of 50 new documents per poll — the LIST count grows with the bucket and not with the change set; below that, HEAD dominates (the 1,000-document row above: HEAD $0.00002 versus LIST $0.000005). An unpaged cold run is dominated by HEAD. A paged cold run squares the LIST count (100 pages times 100 LIST calls at 100,000 documents), so at that size LIST ($0.05) overtakes HEAD ($0.04). A cold run is a one-off event.
- On a self-hosted store the cost is request round trips, not money. A LIST over 100,000 keys is 100 sequential round trips per page per poll because the continuation token chains the pages. The wall time was not measured and no figure is claimed.
- Core deduplicates before it fetches, so a replayed or duplicated entry costs one HEAD in the repository and one existence query in Core, never a document GET.

## 4. Requirements and constraints

| Id | Requirement | Source | Consequence for the design |
|---|---|---|---|
| R1 | At-least-once delivery; duplicates allowed; dedup is the consumer's job | #138 acceptance criteria; Core dedups on `(serialNumber, version)` | Any option may emit duplicates; none may lose an object. |
| R2 | The repository stays stateless: no database, no state outside the bucket | #26 section 4; current architecture | Retry queues, sequence counters and watermarks live in the bucket or in the consumer. |
| R3 | The repository stays a lightweight document store; asset-level query, indexing and aggregation live in Core | ilm#299 | No metadata filters, no per-asset index, no richer statistics in the repository. |
| R4 | Portability across S3 implementations | #121: the MinIO image is discontinued and the replacement is undecided | No dependence on features that Garage lacks or MinIO ships broken; a LIST-based path must remain. |
| R5 | Safe with many replicas | Chart allows HPA up to 100 replicas; requests are handled concurrently even on one replica | No in-memory coordination; no assumption of a single writer. |
| R6 | The legacy `after` timeline of existing objects must not move | Byte compatibility of the unpaged call; Core's watermark | No operation may rewrite existing objects, because a rewrite bumps `LastModified`. |
| R7 | Hourly cadence is the only consumer requirement today | Core's `CbomSyncTask`; the consolidated overview (section 8) describes core#2073 as "extending the existing hourly fetch" | No consumer needs sub-minute latency now. |

## 5. Option (a): keyset cursor `(LastModified, key)` with overlap-and-dedup

### 5.1 Design

- A new optional query parameter `cursor` carries an opaque continuation token. The server builds it as base64url of `v1|<lastModifiedUnixMillis>|<key>` from the LIST entry, not from the HEAD, because the HEAD `Last-Modified` header is an HTTP date at second precision while MinIO listings carry milliseconds. The key is the last field, so a `|` inside a key cannot break parsing.
- Candidates are the listed objects whose pair `(LastModified, key)` is strictly greater than the cursor pair, ordered by `(LastModified asc, key asc)`. The key is unique, so the order is total and the page boundary is exact. The limit becomes hard: the page holds exactly `limit` entries while candidates remain; a candidate skipped after HEAD (404, foreign key) does not count.
- The response body stays a JSON array. The next cursor travels in a `Link` header with `rel="next"` while candidates remain, and the header is absent on the last page. The rule that fewer than `limit` entries means the last page stays valid.
- Validation: `cursor` requires `limit`; `cursor` together with `after` is a 400; a malformed cursor is a 400; `after`-only and `after` plus `limit` calls are unchanged byte for byte.
- Run protocol: the consumer opens a run with `after=watermark&limit=N`, follows `Link` until it is absent, and stores `watermark = run_start - overlap`. Within one run every object that was listable when its page was built is yielded exactly once; across runs duplicates are expected and dedup remains the consumer's job, as today.
- Where the overlap lives. Two placements were considered. Server-side: a run-start call would re-admit every object with `LastModified >= cursor.time - overlap` and rely on consumer dedup. Client-side: the consumer starts each run from `run_start - overlap`, as Core already does. The server-side form is equivalent to `after = cursor.time - overlap` and adds nothing the client rule does not give, while it forces a policy into the server that the server cannot size, because only the consumer knows its own clock skew. This design keeps the overlap on the client. The recommended default stays 60 s, Core's current value, because the store-side upload lasts only the in-cluster transfer of at most 20 MiB by default (`APP_HTTP_MAX_BODY_SIZE`, section 2.1), which leaves most of the 60 s for skew.

### 5.2 What it fixes and what it does not

| Gap class | Effect of option (a) |
|---|---|
| (i) same-second boundary | Fixed exactly by the key tie-break; enables the hard limit and removes the soft-limit overshoot. |
| (ii) open second | Not fixable by any time-ordered listing within a run; covered across runs by the overlap, unchanged. |
| (iii) clock skew | Store-node skew is covered while it fits the overlap; Core-versus-store skew is unchanged because the overlap stays on the client. |
| (iv) metadata | Already closed by deliverable 1 (on the #138 branch, pending merge). |
| (v) deletions | Not addressed. |
| (vi) cost | Unchanged: every page still lists the whole bucket. |

### 5.3 Implementation size and risks

The work builds on deliverable 1's paged path, which already sorts by `(LastModified, key)`: cursor encoding and decoding, handler validation, the `Link` header, tests mirroring the at-least-once suite, OpenAPI and README. No environment variable, no chart change, no store change and no Core change is required for correctness; Core may adopt `limit` and `cursor` later (follow-up issue 2). Risks: a cursor treated as durable across runs or across a store migration (documented as valid within a run); a store whose listing precision changes mid-run (a run lasts minutes, so this is negligible); a client that mixes `after` pages and cursor pages in one run (the protocol forbids it and the validation rejects the combination).

## 6. Option (b): service-assigned monotonic sequence

### 6.1 Design as evaluated

1. Reserve a sequence number: read the counter object `feed/_seq` (value and ETag), write value plus one with `If-Match: <etag>`; on 412 re-read and retry with bounded jittered backoff; if the budget is exhausted, fail the upload with 503 before touching the store.
2. Write the document object as today.
3. Write an index object `feed/<20-digit zero-padded seq>` with `If-None-Match: *`, carrying the document key either in its body or, better, in its own key (`feed/<seq>/<document key>`), so that a LIST alone yields the mapping.
4. The consumer lists `feed/` with `StartAfter=feed/<last seq>` and reads only the new index entries: O(delta) LIST, an exact cursor, no full-bucket scan. Statistics still need one HEAD per new document, so HEAD stays O(delta) as it is today.

### 6.2 Conditional-write support matrix

| Store | `If-None-Match: *` on PUT | `If-Match` on PUT | Available since | Caveats | Confidence |
|---|---|---|---|---|---|
| AWS S3 | Yes | Yes | `If-None-Match` 2024-08-20; `If-Match` 2024-11-25 (both for `PutObject` and `CompleteMultipartUpload`, all bucket types and Regions); `CopyObject` 2025-10-29 | 412 on mismatch, 409 on a concurrent conflict with retry; requires Signature Version 4; "conditional writes do not consider any in-progress multipart uploads"; no per-key request rate is published, only 3,500 writes per second per prefix | Confirmed; per-key throughput unconfirmed |
| MinIO | Yes | Yes | Both since `RELEASE.2023-02-09T05-16-53Z` (PR #16551); the `*` wildcard since `RELEASE.2024-05-07T06-41-25Z` (PR #19682); correctness fixes #21550 and #21567 in `RELEASE.2025-09-07T16-13-09Z` | Issue #21603: a conditional PUT is accepted when read quorum cannot be reached; the fix (PR #21653, merged 2025-10-24) is in no tagged release, the last being `RELEASE.2025-10-15T17-29-55Z`; the project is source-only and archived. The chart pins `RELEASE.2025-09-07T16-13-09Z`, which has the fixes above and the quorum defect. | Confirmed |
| Ceph RGW | Yes | Yes | Present in `rgw_rest_s3.cc`; predates April 2018 | First release carrying it not confirmed | Code confirmed; release unconfirmed |
| Garage | No | No | Never: "structurally impossible" without a consensus algorithm | Option (b) cannot run on Garage | Confirmed |
| SeaweedFS | Yes | Yes | 3.97 (2025-09-01) | Conditions ignored on versioning plus object-lock buckets (issue #8073, v4.07) | Confirmed |
| RustFS | Unconfirmed | Unconfirmed | Not established | Documentation pages unavailable; no release note found | Unconfirmed |
| Cloudflare R2 | Yes | Yes | Documented for `PutObject` | `CompleteMultipartUpload` conditional headers unconfirmed | `PutObject` confirmed |

SDK side: aws-sdk-go-v2 `service/s3` gained `IfNoneMatch` in v1.60.0 (2024-08-20) and `IfMatch` in v1.69.0 (2024-11-25). This repository pins `service/s3 v1.106.0` and `transfermanager v0.3.5`; both expose `IfMatch` and `IfNoneMatch` on their input types (checked in the module cache). How the SDK's automatic retries interact with 409 and 412 was not verified for Go (unconfirmed).

### 6.3 Contention

Every upload performs a CAS on one object. With k uploads racing, one wins each round and the others receive 412 and retry, so the last one needs about k attempts. AWS documents the 412 and 409 semantics with an instruction to retry and publishes no per-key rate, so the sustainable upload rate of a single hot counter is unknown (unconfirmed). Failed conditional requests are billed. Per-replica sub-counters would remove the contention but destroy the total order that is the point of the option, so they were not pursued.

### 6.4 Gap semantics: holes and out-of-order visibility

- A reserved but never written sequence number (crash between steps 1 and 2) is a permanent hole. On its own it is harmless.
- A document written without its index entry (crash between steps 2 and 3) is invisible to the sequence feed forever. Repairing it needs a full LIST reconciliation, which is option (a).
- Out-of-order visibility: request A reserves N, request B reserves N + 1 and writes its index entry first; a consumer that lists at that moment sees N + 1, advances past N, and never sees N when it appears. This happens with a single replica too, because requests run concurrently. The consumer must therefore hold at a hole. A concrete rule (proposed value): do not advance the cursor past a missing sequence number while the youngest present index object above it is younger than 60 s; after that, skip the hole. The grace period must exceed the longest reserve-to-index interval plus reader-versus-store clock skew, which is the same allowance option (a) makes with its overlap, moved from a time window into a hole rule. The server could apply the rule statelessly per request and hide it from consumers, at the price of a feed that stalls for the grace period behind every hole.

### 6.5 Multi-replica behaviour under HPA

Contention grows with replicas times per-replica concurrency. Holes and out-of-order visibility appear at any replica count. Nothing in the option needs in-memory coordination, so it is HPA-safe in the sense of R5, but every consumer inherits the hole rule.

### 6.6 Bucket layout

The counter and the index objects would sit in the document bucket. `store.Search` lists without a prefix and the legacy call fails on any key without a `-`, so `feed/_seq` and `feed/00000000000000000001` would break the byte-compatible call. The clean fix is a second bucket, which means a new environment variable and a chart change; a prefix for documents instead would rename every existing object, and a rename is a copy that bumps `LastModified` (R6).

### 6.7 Backfill of existing objects

Writing index objects for existing keys in `LastModified` order does not touch the originals: the legacy `after` timeline stays intact. Rewriting object metadata instead, through `CopyObject` with `MetadataDirective: REPLACE`, sets `LastModified` to the copy time on every object and replays the whole history into every `after` consumer: the repository HEADs every object again, Core runs one existence query per entry and skips each as a duplicate, and nothing is gained. The same applies to any metadata backfill (section 11).

### 6.8 Rolling upgrade and two writers

During a rolling upgrade an old replica without the feature keeps uploading documents that get no index entry, while new replicas serve the sequence feed. Those documents never appear in the feed. Avoiding it needs a `Recreate` strategy or a maintenance window, and detecting it needs the reconciliation scan of section 6.4 anyway.

### 6.9 Verdict on (b)

Feasible on AWS S3, SeaweedFS 3.97 and later, Ceph RGW and R2; impossible on Garage; unverified on RustFS; on MinIO only with a build that no tagged release provides. It removes the full-bucket LIST but keeps the HEAD fan-out, adds one GET and two conditional PUTs per upload, needs a second bucket, a backfill tool, a hole rule in every consumer and a reconciliation scan as a backstop. At thousands of documents polled hourly it buys nothing that section 3.2 shows is needed.

## 7. Option (c): webhook push (#26)

### 7.1 What it buys

A push on every successful upload gives seconds of latency instead of an hour. It removes the per-poll amplification only if polling stops, and section 7.2 shows polling cannot stop; the honest gain is that the poll can drop to a reconciliation cadence, for example daily, which divides the amplification of section 3.2 by 24 at hourly polling.

### 7.2 Why it cannot be the sole feed

The repository is stateless (R2), so #26's retry policy (`maxRetries`, `backoffMs`) can only be in-memory. A pod restart, an HPA scale-down, a crash during the retry window or a consumer outage longer than the retry budget loses the event permanently. Standard Webhooks retries follow "a retry schedule spanning multiple days" and Stripe retries "for up to three days" because both keep a durable outbox; this service has none and must not grow one (R2). Store-native notifications (S3 event notifications to SNS, SQS, Lambda or EventBridge; MinIO bucket notifications to a webhook target) are the alternative that keeps the repository stateless and out of the delivery path, with a portability caveat: every store has its own event format, targets and guarantees, so the consumer needs an adapter per store, and the choice made under #121 decides what is available. They also share the limitation in a different form: AWS S3 event notifications are "designed to be delivered at least once", "aren't guaranteed to arrive in the same order", may be duplicated, and have no native HTTP target (SNS, SQS, Lambda or EventBridge are required, with FIFO queues excluded); MinIO's webhook target persists to `queue_dir` up to `queue_limit` (default 100000) and "discards new events when the queue is full", and documents no delivery or ordering guarantee (unconfirmed). Every push path is at-least-once and unordered at best, and lossy at worst; the pull feed stays the source of truth.

### 7.3 Authentication

Core has an authentication and permission model; the repository has none, and today the only call direction is Core to repository. A push inverts that direction and makes Core an inbound endpoint that must authenticate the caller. #26 specifies `X-Timestamp` and `X-Nonce` (at least 128 bits of entropy). Those are replay-protection material, not authentication: anyone who can reach the endpoint can send a fresh nonce. The minimum is a signature over the timestamp and the body with a shared secret. Standard Webhooks defines exactly that: headers `webhook-id`, `webhook-timestamp`, `webhook-signature`; HMAC-SHA256 over `msg_id.timestamp.payload`, serialised as `v1,<base64>`; a secret of 24 to 64 bytes with the `whsec_` prefix; the receiver rejects timestamps outside a tolerance (Stripe's default is five minutes) and uses the id as an idempotency key. Mutual TLS is the alternative. The nonce of #26 becomes the `webhook-id`.

### 7.4 Payload

#26 sends the full CBOM in the body. Core fetches the document by URN anyway, and a document can be up to 20 MiB by default (`APP_HTTP_MAX_BODY_SIZE`). A thin event (`serialNumber`, `version`, `created_at`, event type) is sufficient, avoids double transfer, and limits the damage of a forged event to one document GET that must resolve against the store.

### 7.5 Operational cost

Network policy from the repository to Core; a shared secret per consumer with rotation; a Core inbound endpoint with signature verification, timestamp tolerance and an idempotency store; monitoring of delivery failures; and, because there is no durable retry, an agreed statement that delivery is best effort and the poll reconciles. None of that exists today and none of it is inside ilm#299.

## 8. Evaluation matrix

| Criterion | Status quo plus deliverable 1 | (a) keyset cursor | (b) sequence | (c) webhook alone | (a) plus (c) |
|---|---|---|---|---|---|
| Correctness (loss) | No loss with the overlap watermark; soft page boundaries | No loss; exact boundaries within a run | No loss only with the hole rule and a reconciliation scan | Loses events on restart, scale-down or consumer outage | As (a); push is best effort |
| Duplicates | Across runs (overlap) | Across runs (overlap) | On retries and after holes | On retries | As (a) plus push retries |
| Latency | Up to one poll interval (1 h) | Same | Same (poll-driven) | Seconds when it works, unbounded when it does not | Seconds typical, one poll interval worst case |
| LIST and HEAD per poll | `ceil(N / 1000)` LIST per page, HEAD per candidate | Same | O(delta) LIST and HEAD, plus 3 extra requests per upload | None per event, but the reconciliation poll remains | As (a), at a longer poll interval |
| Statefulness | Stateless | Stateless | Counter and index in the bucket; hole rule in consumers | Stateless; retries in memory only | Stateless plus a shared secret |
| Multi-replica safety | Safe | Safe | Safe but contended; holes at any replica count | Safe; duplicates on retries | Safe |
| Portability across S3 implementations | Any S3 with consistent listing | Same | Not Garage; MinIO only on an unreleased build; RustFS unknown | Repository-side push is store-agnostic; store-native push differs per store | Same as (a) |
| Implementation size | On the #138 branch, pending merge | Small (repository only) | Large: counter, index, second bucket, backfill, reconciliation; consumer hole rule | Medium in the repository; medium in Core (endpoint, verification, idempotency) | (a) plus (c) |
| Operational burden | None new | None new | Second bucket, backfill run, store validation per implementation | Secrets, network policy, monitoring, best-effort statement | Highest |
| Risk under rolling upgrade | None | None | Unindexed documents from old replicas | Events lost from terminating pods | Covered by the poll |

## 9. Recommendation

**Decision: adopt option (a), the keyset cursor `(LastModified, key)` with client-side overlap-and-dedup, as the change-feed design; reject option (b) for this epic; defer option (c) as an optional latency layer on top of (a), outside ilm#299.**

Decided by: ____ (Tech Lead)  Date: ____

Reasons:

1. The population is thousands of documents polled hourly. Section 3.2 shows that the polled feed costs cents per month at 100,000 documents, so the amplification that (b) and (c) remove is not a problem the project has.
2. Deliverable 1 already delivers at-least-once with Core's existing watermark rule, and Core already deduplicates. Option (a) completes it with exact page boundaries and a hard limit at small cost, keeps the service stateless (R2), keeps it a document store (R3), runs on every S3 implementation that lists consistently (R4), needs no coordination between replicas (R5) and never touches existing objects (R6).
3. Option (b) trades the LIST scan for a hot counter, holes with a consumer-side grace rule, out-of-order visibility at any replica count, a second bucket, a backfill and a reconciliation scan that is option (a) anyway. Its store support is uneven: impossible on Garage, broken under lost quorum on every tagged MinIO release including the one the chart pins, unknown on RustFS. Those failure modes buy nothing at this scale.
4. Option (c) cannot be the sole feed because a stateless service cannot guarantee delivery, and no consumer needs sub-minute latency (R7). It is worth building only when such a consumer exists, and then only signed and layered on the poll.

Conditions that reopen this decision (proposed values, to be confirmed by the Tech Lead):

| Trigger | Threshold | Reopen |
|---|---|---|
| T1 Document count | More than 50,000 documents in one bucket (50 or more sequential LIST round trips per page per poll) | (b), or a time-prefixed index without a counter (an index object `feed/<lastModifiedMillis>-<key>` written after the document, which gives O(delta) LIST while inheriting (a)'s timing caveats) |
| T2 Poll interval | Any consumer polling more often than every 5 minutes | (b) or the time-prefixed index |
| T3 Latency requirement | A consumer that needs new documents within one minute | (c) on top of (a), with HMAC signing and a Core inbound endpoint |
| T4 Measured poll cost | A poll whose wall time or store throttling is observed to affect the consumer's schedule | (b) or the time-prefixed index |
| T5 Store consistency | The store chosen under #121 does not offer strong list-after-write consistency | Revisit the protocol: the overlap must then also cover the store's visibility lag |
| T6 Second consumer | A consumer that cannot deduplicate on `(serialNumber, version)` | No option in this document serves it; at-least-once stays the contract |
| T7 Delete API | The repository gains a delete operation | The feed needs delete events; neither (a) nor (b) as drafted carries them |

## 10. Scope reconciliation

### 10.1 Issue #26 (webhook delivery): keep open, re-scope

Proposed title: "Optional HMAC-signed webhook notification on top of the polled feed". Proposed edits to the body, numbered as in the companion file's draft 4, which holds the full text:

1. Summary: add a scope statement. The webhook is a latency optimisation layered on the polled feed; `GET /v1/bom` polling remains the source of truth and the only guaranteed path; delivery is best effort with in-memory retries only, so events are lost on pod restart, scale-down or a long consumer outage; the issue is outside ilm#299 and starts when trigger T3 fires.
2. Headers: replace `X-Timestamp` and `X-Nonce` with the Standard Webhooks headers `webhook-id`, `webhook-timestamp`, `webhook-signature` (HMAC-SHA256, `v1,` prefix, `whsec_` secret); `webhook-id` meets the nonce requirement; custom headers stay optional extras, not the authentication mechanism.
3. Body: thin event by default (`event: "cbom.uploaded"`, `serialNumber`, `version`, `created_at`, `cryptoStats`); the full CBOM becomes an opt-in per webhook.
4. Configuration: keep the YAML file and `CBOM_WEBHOOK_CONFIG`; the secret comes from a mounted Kubernetes Secret, never inline.
5. Error handling: keep the rule that failures never block storage or API responses; retries are bounded, in memory, with backoff and jitter; nothing is persisted.
6. Security: receivers verify the signature, reject timestamps outside a five-minute tolerance and deduplicate by `webhook-id`; mutual TLS is an acceptable alternative.
7. Acceptance criteria: the README states that the webhook is not a delivery guarantee; a Core inbound endpoint with signature verification exists before this ships (separate core issue).
8. Optional improvements: `event` becomes mandatory; the test endpoint stays.

### 10.2 Issue #27 (stats as metadata, extended search and upload): close, or reduce to the residue

| #27 item | Disposition | Reason |
|---|---|---|
| Compute statistics on upload and store them as S3 metadata | Shipped | `crypto-stats` user metadata exists; nested counting is on the #138 branch, pending merge. |
| Return statistics in the upload response | Shipped | `BOMCreated.cryptoStats` in `internal/service/upload.go`. |
| Richer statistics (algorithm lists, key sizes, PQC and legacy classes) | Close | Asset-level aggregation belongs to Core (ilm#299); S3 user metadata is also size-capped (limit not verified here, unconfirmed). |
| Filter the search API by metadata | Close | ilm#299 excludes "any asset-level query API or index in cbom-repository"; Core serves all asset queries. |
| Return metadata in search results | Shipped | `cryptoStats` is in every feed entry. |
| Metadata versioning (`cryptoStats.version`) | On the #138 branch, pending merge, with a caveat | `crypto-stats-version: 2` is written to S3 metadata on new uploads. #27 section 8 asked for the version inside `cryptoStats` on the wire; deliverable 1 keeps it in S3 metadata only, by design, so that the JSON stays byte-compatible. It is not on the wire. |
| Tool to recompute metadata for existing CBOMs | Deferred | Section 11 and follow-up issue 6, with the `LastModified` caveat. |

Recommendation: close #27 with a comment carrying this table, and file the recount tool as its own optional issue only if the Tech Lead wants it. The alternative is to reduce #27 to that single item; the companion file's draft 5 holds both texts.

## 11. Stats backfill and recount decision

**Decision: the repository does not rewrite existing objects. Core recounts every CBOM from the document during asset ingest (core#2073) and refreshes the header counts on the `cbom` table. `crypto-stats-version` marks objects counted with the current algorithm. An operator recount tool stays optional and deferred.**

Reasons:

1. Rewriting metadata means `CopyObject` with `MetadataDirective: REPLACE`, which sets `LastModified` to the copy time. Every `after` consumer would then see the entire history again (R6).
2. The replay would not even fix Core: Core skips known `(serialNumber, version)` pairs before it reads anything, so its stored counts would stay stale while the repository pays a full HEAD fan-out and Core one existence query per object.
3. core#2073 parses every component of every CBOM, which makes the header counts a by-product of ingest. Counting there uses the same scope as the repository's version-2 count (`components` tree, depth at most 1000, `cryptographic-asset` with `cryptoProperties`).
4. `crypto-stats-version` is S3 metadata and is not on the JSON wire, so Core cannot tell a legacy-counted entry from a current one through the feed. Treating every repository count as advisory and recounting all of them is the only consistent rule.

Impact on the legacy `after` timeline: none.

The deferred operator tool (follow-up issue 6) would read each object, recompute the statistics and rewrite the metadata. Its caveat must be spelled out in the issue: every rewritten object moves to the head of the `after` timeline, so the tool must run before Core's first asset ingest, when a full pass happens anyway, or the operator accepts a one-off replay of the history into every consumer.

## 12. Follow-up issues

> Filed 2026-09-03: draft 1 → [cbom-repository#144](https://github.com/OmniTrustILM/cbom-repository/issues/144); draft 2 → [core#2208](https://github.com/OmniTrustILM/core/issues/2208); draft 3 → [core#2209](https://github.com/OmniTrustILM/core/issues/2209); draft 4 → proposal posted on [#26](https://github.com/OmniTrustILM/cbom-repository/issues/26#issuecomment-5526007414) (title and body unchanged pending the Tech Lead's decision); draft 5 → disposition posted on [#27](https://github.com/OmniTrustILM/cbom-repository/issues/27#issuecomment-5526007717) (left open for the Tech Lead); draft 6 → [cbom-repository#145](https://github.com/OmniTrustILM/cbom-repository/issues/145) (deferred).

Drafted, not posted, in [2026-09-02-change-feed-follow-up-issues.md](./2026-09-02-change-feed-follow-up-issues.md):

1. cbom-repository: keyset cursor for `GET /v1/bom` (option (a)).
2. core: consume paged repository search and the corrected contract (`limit`, `created_at`, `cryptoStats: null`, `warnings`, watermark rule).
3. core: recount per-CBOM crypto statistics during asset ingest (with or next to core#2073).
4. cbom-repository: #26 re-scope to an optional HMAC-signed webhook on top of the polled feed.
5. cbom-repository: #27 disposition (close, or reduce to the recount tool).
6. cbom-repository, deferred and optional: operator tool to recount statistics of legacy objects.

## 13. Open questions for the Tech Lead

1. Approve option (a) as the design, (b) rejected for this epic, (c) deferred? Yes or no.
2. Documented default overlap: keep 60 s (Core's current value), or raise it to 300 s to absorb unmeasured clock skew? Pick a value.
3. Core's watermark rule: keep `last successful run start - overlap` (recommended: no Core change, and a far-future store timestamp cannot blind the feed), or switch to `max created_at seen - overlap` (independent of Core's clock, but one object with a far-future `LastModified` would move the watermark past real time)? Pick one.
4. #27: close with the disposition table, or keep it open reduced to the recount tool? Pick one.
5. Schedule follow-up issue 1 (the cursor) inside the epic now, or park it until trigger T1 or T2 fires? Pick one.

## 14. References

Confirmed sources from the research note compiled 2026-09-02:

- AWS S3 conditional writes: https://aws.amazon.com/about-aws/whats-new/2024/08/amazon-s3-conditional-writes/ ; https://aws.amazon.com/about-aws/whats-new/2024/11/amazon-s3-functionality-conditional-writes/ ; https://aws.amazon.com/about-aws/whats-new/2025/10/amazon-s3-conditional-write-functionality-copy-operations ; https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html ; https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html ; https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html
- MinIO conditional writes and status: https://github.com/minio/minio/pull/16551 ; https://github.com/minio/minio/releases/tag/RELEASE.2023-02-09T05-16-53Z ; https://github.com/minio/minio/pull/19682 ; https://github.com/minio/minio/releases/tag/RELEASE.2024-05-07T06-41-25Z ; https://github.com/minio/minio/pull/21550 ; https://github.com/minio/minio/pull/21567 ; https://github.com/minio/minio/releases/tag/RELEASE.2025-09-07T16-13-09Z ; https://github.com/minio/minio/issues/21603 ; https://github.com/minio/minio/pull/21653 ; https://github.com/minio/minio/issues/21647
- Other stores: https://github.com/ceph/ceph/blob/main/src/rgw/rgw_rest_s3.cc ; https://garagehq.deuxfleurs.fr/documentation/reference-manual/known-issues/ ; https://github.com/seaweedfs/seaweedfs/pull/7154 ; https://github.com/seaweedfs/seaweedfs/issues/8073 ; https://developers.cloudflare.com/r2/api/s3/api/
- Listing order and consistency: https://docs.aws.amazon.com/AmazonS3/latest/userguide/ListingKeysUsingAPIs.html ; https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html ; https://aws.amazon.com/about-aws/whats-new/2020/12/amazon-s3-now-delivers-strong-read-after-write-consistency-automatically-for-all-applications/ ; https://github.com/minio/minio/blob/master/docs/distributed/README.md
- `LastModified` semantics and precision: https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingMetadata.html#SysMetadata ; https://github.com/aws/aws-cli/issues/5369 ; https://github.com/minio/minio/blob/master/internal/amztime/iso8601_time.go ; https://github.com/minio/minio/blob/master/cmd/api-response.go
- Pricing: https://aws.amazon.com/s3/pricing/ (read 2026-09-02)
- Event notifications: https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-event-types-and-destinations.html ; https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventNotifications.html ; https://aws.amazon.com/blogs/storage/manage-event-ordering-and-duplicate-events-with-amazon-s3-event-notifications/ ; https://github.com/minio/minio/blob/master/docs/bucket/notifications/README.md
- Webhook security: https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md ; https://docs.stripe.com/webhooks
- Keyset pagination: https://use-the-index-luke.com/no-offset ; https://shopify.engineering/pagination-relative-cursors ; https://www.elastic.co/guide/en/elasticsearch/reference/current/paginate-search-results.html#search-after
- aws-sdk-go-v2: https://github.com/aws/aws-sdk-go-v2/blob/main/service/s3/CHANGELOG.md ; https://github.com/aws/aws-sdk-go-v2/blob/main/service/s3/api_op_PutObject.go ; https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager
- Project issues: https://github.com/OmniTrustILM/cbom-repository/issues/138 ; https://github.com/OmniTrustILM/cbom-repository/issues/26 ; https://github.com/OmniTrustILM/cbom-repository/issues/27 ; https://github.com/OmniTrustILM/cbom-repository/issues/121 ; https://github.com/OmniTrustILM/core/issues/2073 ; https://github.com/OmniTrustILM/core/issues/2072 ; https://github.com/OmniTrustILM/ilm/issues/299
