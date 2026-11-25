# OpenAPI route generation

Kiterunner's `pkg/openapi` package parses Swagger 2.0 and OpenAPI 3.x documents
and converts them into the existing `proute` intermediate representation. No
separate scan path is introduced: quick scans, full scans, `--filter-api`,
status-code filters, wildcard detection and progress reporting all operate on
the generated routes exactly as they do for `.kite` wordlists.

## Supported input

- Swagger 2.0 and OpenAPI 3.0/3.1 documents
- JSON and YAML
- Local references under `#/definitions`, `#/parameters`,
  `#/components/schemas`, `#/components/parameters`,
  `#/components/requestBodies` and other local JSON pointers
- `basePath` and `servers[].url`, including server variables with defaults
- Path-level and operation-level parameters, with operation values overriding
  path-item values
- Swagger `body`/`formData` parameters and OpenAPI 3 `requestBody` content
- `allOf`, `oneOf`/`anyOf` representative schemas, arrays, enums, defaults,
  examples and recursive models (recursion is cut off safely)
- API key, basic, bearer and OAuth/OpenID security schemes

Hop-by-hop methods (`HEAD`, `OPTIONS`, `TRACE`) are generated in the coverage
report but intentionally remain outside the scanned request set, matching
kiterunner's pre-existing behavior.

## Commands

```bash
# Direct scanning: -w auto-detects OpenAPI JSON/YAML
kr scan https://target.com -w spec.yaml

# Explicit spec flag plus report output
kr scan https://target.com --openapi spec.json \
  --openapi-report --openapi-report-file coverage.json

# Compile to .kite, then use it with any existing scan mode
kr kb compile spec.yaml spec.kite
kr scan targets.txt -w spec.kite --kitebuilder-full-scan

# Human-readable coverage report
kr kb openapi-report spec.yaml --manual manual.routes.txt

# Machine-readable report(s)
kr kb openapi-report spec1.yaml spec2.json --json --output coverage.json
```

Manual baseline files contain one route per line. `METHOD` is optional and
defaults to `GET`; comments start with `#`:

```text
GET /v2/pet/{petId}
POST /v2/pet
/health
```

Comparison canonicalises parameter names, so `/pets/{petId}` and `/pets/{id}`
match. Extra and missing routes are reported separately.

## Verified coverage

Three representative specs are checked into `pkg/openapi/testdata` together
with independent, hand-written route lists. The test
`TestThreeSpecsMatchManualBaseline` asserts 100% coverage for each one:

| Spec | Version | Operations | Generated | Scannable | Manual baseline | Manual match |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Petstore | Swagger 2.0 | 6 | 6 | 6 | 6 | 100% |
| Petstore | OpenAPI 3.0.3 | 11 | 11 | 11 | 11 | 100% |
| Bookstore | OpenAPI 3.0.3 | 9 | 9 | 9 | 9 | 100% |

The bookstore fixture also covers relative server URLs with variables,
`allOf` request bodies, bearer security and recursive `Author`/`Book` schemas.
The OpenAPI 3 petstore fixture covers YAML parsing, `components` parameter and
request-body references, multi-content request bodies and path-level
parameters.
