# Change feed follow-up issues: drafts

Companion to [2026-09-02-change-feed-decision.md](./2026-09-02-change-feed-decision.md) (cbom-repository#138, deliverable 2). Written 2026-09-02.

These drafts were filed on 2026-09-03: draft 1 as [cbom-repository#144](https://github.com/OmniTrustILM/cbom-repository/issues/144), draft 2 as [core#2208](https://github.com/OmniTrustILM/core/issues/2208), draft 3 as [core#2209](https://github.com/OmniTrustILM/core/issues/2209), draft 4 as a proposal comment on [#26](https://github.com/OmniTrustILM/cbom-repository/issues/26#issuecomment-5526007414) (issue text unchanged pending approval), draft 5 as a disposition comment on [#27](https://github.com/OmniTrustILM/cbom-repository/issues/27#issuecomment-5526007717) (left open), draft 6 as [cbom-repository#145](https://github.com/OmniTrustILM/cbom-repository/issues/145) (deferred). The texts below are the drafts as filed. Abbreviations: S3 (Amazon Simple Storage Service and its API), CBOM (Cryptography Bill of Materials), HMAC (hash-based message authentication code), PQC (post-quantum cryptography), URN (Uniform Resource Name), TLS (Transport Layer Security). The numbering matches section 12 of the decision document. Each draft names the target repository, a suggested title and labels that exist in that repository today, and a body in the style of #138 (`### Description`, `### Acceptance Criteria` with checkboxes, `### Out of scope` where useful). Drafts 4 and 5 are proposed edits to existing issues rather than new issues.

---

## Draft 1: keyset cursor for `GET /v1/bom` (change feed option (a))

- Target repository: OmniTrustILM/cbom-repository
- Suggested title: Keyset cursor for GET /v1/bom (change feed option (a))
- Labels: `enhancement`
- Related: #138 (decision, section 5), ilm#299, core#2073

### Description

#138 decided the change feed: a keyset cursor on `(LastModified, key)` with overlap on the consumer side and deduplication in Core (see `docs/design/2026-09-02-change-feed-decision.md`, sections 5 and 9). Deliverable 1 of #138 (on the #138 branch, pending merge) adds the paged `limit` mode with a soft limit that never splits a second. This issue completes it with an exact continuation token so that page boundaries are unique and the limit becomes hard.

Design, as decided:

- New optional query parameter `cursor`: opaque, base64url of `v1|<lastModifiedUnixMillis>|<key>`, built by the server from the LIST entry's `LastModified` (native precision: seconds on AWS, milliseconds on MinIO) and the object key. The key is the last field.
- Candidates are the listed objects whose `(LastModified, key)` is strictly greater than the cursor pair, ordered by `(LastModified asc, key asc)`. The page holds exactly `limit` entries while candidates remain. Candidates skipped after HEAD (404, foreign key without `-`) do not count.
- The next cursor is returned in a `Link` header with `rel="next"` while candidates remain; the header is absent on the last page. The body stays a JSON array of the existing entry shape.
- `cursor` requires `limit`. `cursor` combined with `after` is a 400. A malformed cursor is a 400. Calls with `after` only, or `after` plus `limit`, are unchanged byte for byte, including the soft limit.
- The overlap stays on the client: a run opens with `after=watermark&limit=N`, follows `Link`, and stores `watermark = run_start - overlap`. The cursor is valid within a run only.

### Acceptance Criteria

- [ ] `GET /v1/bom?after=` and `GET /v1/bom?after=&limit=` responses are byte-identical to the deliverable 1 behaviour (golden tests kept).
- [ ] `GET /v1/bom?cursor=&limit=` returns exactly `limit` entries while candidates remain and fewer on the last page; skipped candidates do not count toward the limit.
- [ ] Entries are ordered by `(LastModified asc, key asc)` and every entry's pair is strictly greater than the cursor pair.
- [ ] The `Link: <...>; rel="next"` header is present exactly when more candidates remain; the cursor it carries is built from the LIST `LastModified`, not from the HEAD value.
- [ ] 400 problem+json for: malformed cursor, `cursor` without `limit`, `cursor` together with `after`, `limit` outside 1 to 1000.
- [ ] Tests mirror the deliverable 1 at-least-once suite: within one run each object appears at most once and every object listable when its page was built appears exactly once; objects inserted into the boundary second and into later seconds between page fetches; a seeded randomized run with many same-second objects and random limits; second-precision (AWS style) and millisecond-precision (MinIO style) timestamps; HEAD 404 skipped without counting; foreign key skipped with an error log.
- [ ] OpenAPI (`getBOMs`): `cursor` parameter, `Link` response header, the 400 cases, and the run protocol with client-side overlap and the statement that a cursor is valid within a run.
- [ ] README search section updated accordingly.
- [ ] Sonar quality gate passes; at least 80 % coverage on new code.

### Out of scope

Server-side overlap; sequence numbers or index objects (option (b), rejected in #138); delete events; response envelopes or any change to the entry shape; new environment variables or chart changes; changes to Core (draft 2).

---

## Draft 2: consume paged repository search and the corrected contract

- Target repository: OmniTrustILM/core
- Suggested title: Consume paged cbom-repository search and the corrected feed contract (limit, created_at, null cryptoStats)
- Labels: `enhancement`
- Related: cbom-repository#138, core#2073, draft 3

### Description

cbom-repository 0.2.0 (#138, deliverable 1, on the #138 branch, pending merge) changes the search contract that `CbomSyncTask` and `CbomServiceImpl.sync()` consume through `CbomRepositoryClient`:

- `GET /api/v1/bom?after=` accepts an optional `limit` (1 to 1000). With `limit`, entries are sorted by `(created_at, key)` and the client protocol is: advance `after` to the last `created_at` within a run, stop on a page shorter than `limit`, and between runs use `watermark = run_start - overlap`. Core already does the last step (last successful run start minus 60 s).
- The entry field is `created_at`, not `timestamp`. `BomEntryDto.timestamp` is never populated today.
- Objects with missing or invalid statistics become visible with `cryptoStats: null` and `warnings: ["crypto-stats-missing"]` or `["crypto-stats-invalid"]` instead of being hidden. If Core dereferences `cryptoStats` without a null check, the entry throws, is counted as skipped, and is never offered again because the watermark moves past it at the next successful run. Baseline: today the repository drops such objects, so Core never saw them; a null-dereference skip leaves the identical end state (Core catches per-entry failures and the run still counts as successful), so there is no regression, and for invalid metadata the change is a strict improvement because one bad object no longer fails the whole feed with 500. Handling the null so that the entry is recovered is this issue's job.
- `version` can be the string `original`; Core already filters `original` out.
- The repository's statistics are advisory: legacy objects hold a shallow count and the JSON does not say which (the `crypto-stats-version` marker is S3 metadata only). Draft 3 recounts during ingest.

The decision document (`docs/design/2026-09-02-change-feed-decision.md`, section 13, question 3) recommends keeping the watermark rule `last successful run start - overlap`; this issue assumes that choice unless the Tech Lead picks the alternative.

### Acceptance Criteria

- [ ] `CbomRepositoryClient` sends `limit` (proposed value 1000) and iterates pages per the documented protocol; a short page terminates the run; an empty first page is a valid empty run.
- [ ] `BomEntryDto` maps `created_at`; the dead `timestamp` field is removed or annotated as the JSON name `created_at`.
- [ ] `cryptoStats: null` and `warnings` are tolerated: no exception; the warning codes are logged with the entry identity; the entry is still stored, with counts left for the recount of draft 3 or computed from the fetched document.
- [ ] A skipped entry is not lost silently: skipped identities `(serialNumber, version)` are recorded with the failure reason and retried on the next 3 runs (default, configurable); after that they are recorded as permanently skipped and the watermark advances as today. A run with skipped entries still counts as successful, so one permanently failing entry (a corrupt document, a Core validation bug) can never freeze the watermark.
- [ ] The watermark rule stays `last successful run start - overlap`; the overlap is configurable with a default of 60 s and is documented.
- [ ] Deduplication on `(serialNumber, version)` is unchanged.
- [ ] Tests against a stubbed repository cover: multiple pages, short-page termination, `cryptoStats: null` with each warning code, `version: "original"`, and the bounded retry of skipped identities (retried on 3 runs, then permanently skipped).
- [ ] When cbom-repository ships the cursor (draft 1), adopting `cursor` and `Link` is a one-line follow-up noted on the ticket, not a blocker.

### Out of scope

Asset ingest and tombstones (core#2073); the recount itself (draft 3); any webhook receiver (draft 4).

---

## Draft 3: recount per-CBOM crypto statistics during asset ingest

- Target repository: OmniTrustILM/core
- Suggested title: Recount per-CBOM cryptographic statistics from the walked components during asset ingest
- Labels: `enhancement`
- Related: core#2073 (part of, or adjacent to), core#2072, cbom-repository#138 (decision section 11), draft 2

### Description

The counts on the `cbom` table come from the repository's `cryptoStats` feed entry. cbom-repository#138 decided (section 11 of `docs/design/2026-09-02-change-feed-decision.md`) that the repository will not rewrite existing objects: objects uploaded before the counting fix keep a shallow count (nested components not counted), new uploads carry `crypto-stats-version: 2` in S3 metadata, and that marker is not on the JSON wire. Core therefore cannot tell a legacy count from a current one and must treat every repository count as advisory.

core#2073 walks every component of every CBOM (iterative, depth-first, document order, depth at most 1000, `components` tree only, as in `CbomAssetExtractor` and `DocumentScope.walk`). The header counts are a by-product of that walk. This issue recounts them there and refreshes the `cbom` table.

### Acceptance Criteria

- [ ] During the per-CBOM unit of work, the counts are computed from the walked components with the same rules as the repository's version-2 count: `type == cryptographic-asset` with a non-null `cryptoProperties`, `components` tree only (not `metadata.component`, `metadata.tools`, `formulation`, `services`), depth at most 1000.
- [ ] The computed counts replace the repository-provided counts on the `cbom` row for that CBOM; the repository values are used only until the first ingest of that CBOM has run.
- [ ] `cryptoStats: null` from the feed leaves the counts unset until ingest, with no error.
- [ ] The statistics and sync-visibility endpoints shipped by core#2145 read the recounted values.
- [ ] Test: a CBOM whose nested components hold three cryptographic assets under a library, fed with a legacy shallow count of one, ends with three after ingest.
- [ ] The ticket states whether this lands inside core#2073 or as a follow-up immediately after it.

### Out of scope

Any rewrite of repository objects or metadata (draft 6, deferred); changes to the repository's counting rules; PQC verdicts (core#2151).

---

## Draft 4: #26 re-scope, optional HMAC-signed webhook on top of the polled feed

- Target repository: OmniTrustILM/cbom-repository
- Issue: #26 (existing; keep open)
- Suggested new title: Optional HMAC-signed webhook notification on top of the polled feed
- Labels: `enhancement` (unchanged)
- Related: #138 (decision sections 7, 9 and 10.1), ilm#299 (outside its scope)

### Proposed edits to the issue text

1. Add at the top, under Summary: "Scope decision (#138, 2026-09-02): the webhook is a latency optimisation layered on the polled feed `GET /v1/bom`. Polling remains the source of truth and the only guaranteed path. Delivery is best effort: the service is stateless, retries are in memory only, and events are lost on pod restart, scale-down or a consumer outage longer than the retry budget. Consumers reconcile through the poll. This issue is outside ilm#299 and starts when a consumer needs sub-minute latency (trigger T3 in the decision document)."
2. Section 2, mandatory headers: replace `X-Timestamp` and `X-Nonce` with the Standard Webhooks headers `webhook-id` (unique per event; satisfies the 128-bit nonce requirement), `webhook-timestamp` (Unix seconds) and `webhook-signature` (`v1,<base64 HMAC-SHA256>` over `webhook-id.webhook-timestamp.body`). Custom headers stay optional and are not an authentication mechanism.
3. Section 2, request body: default to a thin event `{ "event": "cbom.uploaded", "serialNumber", "version", "created_at", "cryptoStats" }`; the full CBOM content becomes an opt-in flag per webhook (`includeDocument: true`), because the consumer fetches the document by URN anyway and a document can be up to the configured body cap.
4. Section 3, configuration: keep the YAML file and `CBOM_WEBHOOK_CONFIG`; add `secretRef` pointing to a mounted Kubernetes Secret holding the `whsec_` secret (24 to 64 bytes); forbid inline secrets in the YAML.
5. Section 5, error handling: keep "Webhook failures must not block CBOM storage or API responses"; replace the retry wording with "bounded in-memory retries with exponential backoff and jitter; no persistence; exhausted retries are logged and counted, not queued".
6. Section 6, security: add "receivers must verify the signature, reject `webhook-timestamp` outside a tolerance of five minutes, and deduplicate by `webhook-id`"; keep TLS verification and the no-logging rule for secrets; add mutual TLS as an acceptable alternative to HMAC.
7. Section 7, acceptance criteria: add "the README states that the webhook is not a delivery guarantee and that `GET /v1/bom` polling remains the feed"; add "a Core inbound endpoint with signature verification exists before this ships (separate core issue)".
8. Section 8, optional improvements: promote `event` to mandatory (edit 3); keep the test endpoint.

### Acceptance Criteria (for the re-scope itself)

- [ ] The eight edits above are applied to #26 and the title is changed.
- [ ] A dated comment on #26 links the decision document and names trigger T3.
- [ ] A Core issue "Inbound endpoint for cbom-repository webhook events" is filed and cross-linked when #26 is scheduled, not before.

---

## Draft 5: #27 disposition

- Target repository: OmniTrustILM/cbom-repository
- Issue: #27 (existing)
- Recommended action: close, with the comment below. Alternative: keep open, reduced to the recount tool, with the shorter comment further down.
- Labels: on closing, add none (the existing `enhancement` label stays); if reduced, keep `enhancement`
- Related: #138 (decision section 10.2 and section 11), ilm#299

### Proposed closing comment

"Disposition per cbom-repository#138 (design decision, 2026-09-02, `docs/design/2026-09-02-change-feed-decision.md` section 10.2):

| Item in this issue | Disposition | Reason |
|---|---|---|
| Statistics computed on upload and stored as S3 metadata | Shipped | `crypto-stats` metadata; nested components counted on the #138 branch, pending merge |
| Statistics in the upload response | Shipped | `cryptoStats` in the upload response body (`BOMCreated`) |
| Richer statistics (algorithms, key sizes, PQC and legacy classes) | Closed | Asset-level aggregation is Core's responsibility under ilm#299 |
| Metadata-filtered search | Closed | ilm#299 excludes any asset-level query API or index in cbom-repository; Core serves asset queries |
| Metadata in search results | Shipped | `cryptoStats` in every `GET /v1/bom` entry |
| Metadata versioning | On the #138 branch, pending merge, with a caveat | `crypto-stats-version: 2` in S3 metadata on new uploads. Section 8 of this issue asked for `cryptoStats.version` on the wire; #138 keeps it in S3 metadata only, by design, to keep the JSON byte-compatible |
| Tool to recompute metadata for existing CBOMs | Deferred | Tracked separately if wanted; see the caveat in #138 section 11: a rewrite moves every object to the head of the `after` timeline |

Every item has a home. Closing this issue; reopen the recount tool as its own issue if an operator needs it."

### Alternative: reduced scope

If the Tech Lead prefers to keep #27 open: retitle it "Operator tool to recount statistics of legacy objects", replace the body with draft 6, and post the table above as a comment explaining what was removed and why.

---

## Draft 6 (deferred, optional): operator tool to recount statistics of legacy objects

- Target repository: OmniTrustILM/cbom-repository
- Suggested title: Operator tool to recount crypto statistics of legacy objects (rewrites metadata; replays the after timeline)
- Labels: `enhancement`
- Related: #138 (decision section 11), #27, draft 3
- Status: deferred; file only if an operator asks for repository-side counts that match the current algorithm

### Description

Objects uploaded before #138 deliverable 1 carry a shallow `crypto-stats` count and no `crypto-stats-version` metadata. #138 decided that the repository does not rewrite them and that Core recounts during asset ingest (draft 3). This tool exists only for operators who want the repository's own metadata corrected, for example for a consumer other than Core.

Mechanism: list the bucket; for every object without `crypto-stats-version`, fetch the document, run `CalculateCryptoStats`, and rewrite the metadata with `CopyObject` onto the same key using `MetadataDirective: REPLACE`, adding `crypto-stats-version: 2`.

Caveat that the issue must state up front: `CopyObject` sets `LastModified` to the copy time. Every rewritten object moves to the head of the `after` timeline, so every consumer of `GET /v1/bom` sees the whole rewritten history again at its next run. Core deduplicates on `(serialNumber, version)` before fetching, so the replay costs Core one existence query per entry and the repository one HEAD per object, and Core's stored counts are not corrected by it. The tool must therefore run before Core's first asset ingest (core#2073), when a full pass over all CBOMs happens anyway, or the operator accepts a one-off replay.

### Acceptance Criteria

- [ ] Command-line tool (or a `Makefile` target) that runs against a bucket with the existing `APP_S3_*` configuration, with `--dry-run` as the default and an explicit `--apply` flag.
- [ ] Only objects without `crypto-stats-version` are rewritten; objects already at version 2 are skipped and counted.
- [ ] Each rewrite keeps the document bytes and the `version` metadata unchanged and adds `crypto-stats-version: 2`.
- [ ] The tool prints, before applying, the number of objects it will rewrite and the sentence "every rewritten object will reappear in every consumer's next `GET /v1/bom` run".
- [ ] The README section for the tool carries the caveat above and the instruction to run it before Core's first asset ingest or to accept the replay.
- [ ] Tests with the in-memory S3 fake: dry run changes nothing; apply rewrites exactly the legacy objects; `LastModified` of rewritten objects moves forward (asserting the caveat, not hiding it).

### Out of scope

Any change to the feed contract; any automatic or scheduled execution inside the service; recounting inside Core (draft 3).
