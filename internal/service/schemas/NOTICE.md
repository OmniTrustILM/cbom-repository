# Third-party schema attribution

The JSON Schema files in this directory are vendored (redistributed unmodified)
from the **CycloneDX specification** project and are licensed under the
**Apache License, Version 2.0** — see the [`LICENSE`](./LICENSE) file in this
directory for the full text.

Vendored files:

- `bom-1.6.schema.json`
- `bom-1.7.schema.json`
- `spdx.schema.json`
- `jsf-0.82.schema.json`
- `cryptography-defs.schema.json`

- **Source:** https://github.com/CycloneDX/specification (published at
  http://cyclonedx.org/schema/)
- **Copyright:** © OWASP Foundation and the CycloneDX contributors.
- **License:** Apache License, Version 2.0 (http://www.apache.org/licenses/LICENSE-2.0)

These files are bundled so that BOM schema validation is fully self-contained and
requires no network access at runtime (see `internal/service/service.go`). They are
redistributed as-is; no modifications have been made to their contents.
