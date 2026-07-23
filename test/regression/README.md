# Local regression environment — cbom-repository (+ core notes)

Self-contained environment to regression-test **cbom-repository** (built from the
current working tree, i.e. the branch under test) against a real MinIO S3 backend.

## Bring it up / tear it down

```bash
cd test/regression
docker compose up -d --build      # minio + bucket "czert" + cbom-repository on :8080
bash run.sh                       # run the regression checks
REG_OVERSIZE=1 bash run.sh        # also run the ~21 MiB oversized-body (413) check
docker compose down -v            # stop + clean
```

- App: `http://localhost:8080/api` (routes under `/api/v1/bom`). MinIO console: `http://localhost:9001` (minioadmin/minioadmin).
- Upload contract: `Content-Type: application/vnd.cyclonedx+json` with an optional `; version=<1.6|1.7>`. When `version` is omitted, the server auto-detects the version from the document's `specVersion`.

## What `run.sh` covers

Positive: 1.6 upload, 1.7 upload, retrieve 1.7 (byte-preserved), list versions, search.
Negative: version/body mismatch → 400, unsupported version (1.4) → 400, unknown `specVersion` (1.8) → 400, wrong base media type → 415, oversized → 413 (optional).
Integration: simulates core's `CbomRepositoryClient` request shape (no `version=` param) — the server auto-detects the version from the document's `specVersion`, so both 1.6 and 1.7 bodies are accepted.

Sample docs: `samples/bom-1.6.json`, `samples/bom-1.7.json` (each a crypto-asset; the 1.7 one carries the 1.7-only `algorithmFamily`).

## core (the other half) — why it isn't in this compose

`../core` (OmniTrust Core) is a **platform service**, not a standalone app.
Its `application.yml` requires, with no defaults (so it will not boot without them):
`JDBC_URL`/`JDBC_USERNAME`/`JDBC_PASSWORD` (Postgres + Flyway schema `core`),
`BROKER_URL` (RabbitMQ), `AUTH_SERVICE_BASE_URL`, `OPA_BASE_URL`, `SCHEDULER_BASE_URL`,
`PROVISIONING_API_URL`, and trust-store settings. Its REST APIs are also
authenticated. And the cbom-repository URL is **not** an env var — core reads it from a
DB **platform setting** (`platformSettings...getCbomRepositoryUrl()`), configured at
runtime, not from config.

So a real "core + cbom-repository" end-to-end requires standing up the whole platform
(auth-service, OPA, scheduler, provisioning, RabbitMQ, Postgres, trust stores) via the
platform's own deployment (compose/helm), then setting the **CBOM Repository URL**
platform setting to point at this cbom-repository (e.g. `http://cbom-repository:8080/api`).

### Practical options for regression testing the integration
1. **Contract simulation (included):** `run.sh` already reproduces core's exact
   `CbomRepositoryClient` request shape (no `version=` param) against the live
   cbom-repository, confirming version auto-detection accepts both 1.6 and 1.7
   without the platform.
2. **Full platform:** bring up the OmniTrust platform deployment, add
   `cbom-repository` (this compose's app service) to that network, and set the CBOM
   Repository URL platform setting. Exercise core's `/v1/cbom` endpoints.
