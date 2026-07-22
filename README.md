# CBOM-Repository

The CBOM Repository service acts as an object storage wrapper built on top of an S3-compatible backend.
It provides a convenient REST API for uploading, retrieving, and searching Cryptographic Bills of Materials (CBOM) documents.

## Installation

Use the provided Helm chart to deploy the service into your Kubernetes cluster.
Please refer to the [Helm chart README](./deploy/charts/cbom-repository/README.md) for detailed installation instructions and configuration options.

## Status Work In Progress

This project is currently under active development.

* You can explore the current REST API design at [OpenAPI Spec](./api/openapi.yaml).
* To run the service locally or see development notes, please continue to the development guide [here](./DEV.md).

## API Endpoints

A summary of the available endpoints and methods are below. For the complete specification please see [OpenAPI Spec](./api/openapi.yaml).
Please note that HTTP API Paths have an additional default prefix `/api`. You can change it by setting the environment variable `APP_HTTP_PREFIX`.

| Path | HTTP Method | Required Params | Optional Params | Description |
|:-----|:------------|:----------------|:----------------|:------------|
| `/v1/bom`       | `POST` | Contents of BOM in request body and `Content-Type` header set | | Uploads the supplied BOM to the repository |
| `/v1/bom`       | `GET`  | query parameter `after` | | Retrieves a list of BOM serial numbers and versions that were created later that `after` timestamp |
| `/v1/bom/{urn}` | `GET`  | | query parameter `version` | If optional query parameter `version` is not supplied, retrieves the latest version of the BOM from repository |
| `/v1/bom/{urn}/versions` | `GET` | | | List all available versions of a BOM identified by its URN |

Let's see each endpoint in greater detail.

### POST /v1/bom (Upload)

The upload operation requires a valid `Content-Type` header. At this time, only JSON format is supported, and CycloneDX **1.6 and 1.7** are supported.
This means the `Content-Type` header must be set to: 
```
application/vnd.cyclonedx+json
```

Specify the version via the media type, e.g.:
```
Content-Type: application/vnd.cyclonedx+json; version=1.7
```

When the `version` parameter is omitted, the server uses the configured default (`APP_DEFAULT_BOM_VERSION`, default `1.6`).
The server validates the document against the **declared** version; a body whose `specVersion` disagrees with the declared version is rejected with 400.

#### Upload behavior

When processing uploaded BOMs, the system recognizes several use cases:

* **BOM includes both a serial number and a version.**
  The BOM is stored exactly as provided. Subsequent attempts to upload the same serial number and version will result in a 409 Conflict response.
* **BOM includes a serial number but no version.**
  The storage layer is checked for existing BOMs with the same serial number: 
  * If matching entries are found, the uploaded BOM is assigned the next version number (latest version + 1).
  * If none exist, the uploaded BOM is stored as Version 1.
* **BOM includes neither a serial number nor a version.**
  A new URN is generated automatically. Two BOMs are stored:
  1. The original, potentially cryptographically signed, stored under the new URN with version original.
  2. A normalized version, where a serial number and version have been assigned, stored under the same URN with version 1.

Upon successful upload, the endpoint returns basic cryptographic statistics about the provided BOM.

This feature is still a work in progress, and both the format and the details reported may evolve over time.

### GET /v1/bom (Search)

The search operation requires a single query parameter: `after`, whose value must be a Unix timestamp.
The endpoint responds with a list of URNs along with all versions created after the specified timestamp.
This allows clients to efficiently discover updates without scanning the entire BOM collection.

### GET /v1/bom/{urn} (Get by URN)

The get operation retrieves the latest version of a BOM—i.e., the entry with the highest version number—based on the {urn} supplied in the URL path.

The value of {urn} must conform to RFC 4122, meaning it follows the format:
```
urn:<NID>:<NSS>
```
Where:
* `<NID>` — Namespace Identifier, which must be exactly uuid for RFC 4122.
* `<NSS>` — Namespace-Specific String, which must be a valid UUID.

To retrieve a specific version instead of the latest, you may provide the optional query parameter:
```
?version=<number>
```

## Full list of environment variables

The following environment variables are used to configure the `CBOM-Repository`:

| Environment Variable | Required | Default Value | Explanation |
|:---------------------|:---------|:--------------|:------------|
| `APP_LOG_LEVEL` | ![](https://img.shields.io/badge/-YES-success.svg) | `INFO` | logger level, possible values: `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `APP_HTTP_PORT` | ![](https://img.shields.io/badge/-YES-success.svg) | `8080` | HTTP server port |
| `APP_HTTP_PREFIX` | ![](https://img.shields.io/badge/-YES-success.svg) | `/api` | HTTP server handlers route prefix, mainly used to mount the CBOM Repository handlers under a different starting path |
| `APP_S3_ACCESS_KEY` | ![](https://img.shields.io/badge/-YES-success.svg) | | s3-compatible store access key |
| `APP_S3_SECRET_KEY` | ![](https://img.shields.io/badge/-YES-success.svg) | | s3-compatible store secret key |
| `APP_S3_REGION` | ![](https://img.shields.io/badge/-YES-success.svg) | | s3-compatible store Region |
| `APP_S3_ENDPOINT` | ![](https://img.shields.io/badge/-NO-red.svg) | | s3-compatible store endpoint, leave empty for aws roles or default aws env. variables to take precedence |
| `APP_S3_BUCKET` | ![](https://img.shields.io/badge/-YES-success.svg) | | bucket name |
| `APP_S3_USE_PATH_STYLE` | ![](https://img.shields.io/badge/-YES-success.svg) | `true` | Use s3 path style |
| `APP_DEFAULT_BOM_VERSION` | ![](https://img.shields.io/badge/-YES-success.svg) | `1.6` | CycloneDX version assumed when an upload's Content-Type omits the `version` parameter; must be a supported version or the service refuses to start |
