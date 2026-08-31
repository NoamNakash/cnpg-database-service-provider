# Test Plan: CNPG Database SP — Unit Tests

## Overview

- **Related Spec:** .ai/specs/cnpg-database-sp.spec.md
- **Related Requirements:** REQ-HTTP-050, REQ-HTTP-091, REQ-HTTP-090, REQ-HLT-010–080, REQ-API-010–240, REQ-STR-010, REQ-STR-080, REQ-CNPG-050, REQ-CNPG-060, REQ-MON-050–100, REQ-MON-110–120, REQ-MON-130, REQ-MON-145, REQ-MON-150, REQ-MON-160, REQ-MON-165, REQ-MON-170, REQ-XC-ID-010–020, REQ-XC-ERR-010–040, REQ-XC-CFG-010–030, REQ-REG-010
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-08-19
- **Last Updated:** 2026-08-19

Unit tests verify individual components in isolation. All external dependencies
(DatabaseRepository, K8s client, NATS, HTTP server) are replaced with mocks,
fakes, or test doubles. Tests use `httptest.NewRecorder` for handler tests and
direct function calls for pure logic.

### Utility Test Case Approach

Utility and helper functions (resource conversion, error types, event
construction, debounce, indexer functions, label builders) are **not** tested
in dedicated test classes. Instead:

- Each utility behaviour retains a **TC-ID** for requirements traceability.
- The TC-ID is **referenced** in the higher-level behavioural test(s) that
  exercise the utility transitively.
- All utility TC-IDs, their descriptions, and cross-references are collected in
  the [Utility Test Case Index](#utility-test-case-index) at the end of this
  document.

---

## 1 · Configuration

> **Suggested Ginkgo structure:** `Describe("Configuration")`

### TC-U002: Load configuration from environment variables

- **Requirement:** REQ-HTTP-050
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_SERVER_ADDRESS=":9090"` and `SP_SERVER_SHUTDOWN_TIMEOUT="30s"` are set
- **When:** Config is loaded
- **Then:** The loaded config has `server.address = ":9090"` and `shutdownTimeout = 30s`

### TC-U004: Default values applied when no config specified

- **Requirement:** REQ-HTTP-050
- **Priority:** Medium
- **Type:** Unit
- **Given:** No environment variables are set
- **When:** Config is loaded
- **Then:** `server.address` defaults to `":8080"`, `shutdownTimeout` defaults to `15s`, and `cnpg.defaultVersion` defaults to `18` (note: `SP_CNPG_EXTERNAL_SERVICE_TYPE` has no default — validated as required by TC-U082)

### TC-U082: SP_CNPG_EXTERNAL_SERVICE_TYPE is required at startup

- **Requirement:** REQ-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE` is not set (absent)
- **When:** Config is loaded
- **Then:** An error is returned identifying `SP_CNPG_EXTERNAL_SERVICE_TYPE` as required

### TC-U083: SP_CNPG_EXTERNAL_SERVICE_TYPE rejects invalid values

- **Requirement:** REQ-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE` is set to `"ClusterIP"` (invalid)
- **When:** Config is loaded
- **Then:** An error is returned stating the value must be `LoadBalancer` or `NodePort`

### TC-U084: SP_CNPG_EXTERNAL_SERVICE_TYPE accepts LoadBalancer

- **Requirement:** REQ-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE="LoadBalancer"`
- **When:** Config is loaded
- **Then:** Config loads successfully with `externalServiceType="LoadBalancer"`

### TC-U085: SP_CNPG_EXTERNAL_SERVICE_TYPE accepts NodePort

- **Requirement:** REQ-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE="NodePort"`
- **When:** Config is loaded
- **Then:** Config loads successfully with `externalServiceType="NodePort"`

### TC-U063: Config loading fails when required fields are absent

- **Requirement:** REQ-XC-CFG-020
- **Priority:** High
- **Type:** Unit
- **Given:** Required config values (SP_NATS_URL, SP_NAME, SP_CNPG_EXTERNAL_SERVICE_TYPE) are absent
- **When:** Config is loaded
- **Then:** An error is returned identifying each missing required field
- **Referenced by:** TC-U002 (configuration loading)

---

## 2 · (Reserved)

---

## 3 · Database API Handlers

> **Suggested Ginkgo structure:** `Describe("Database API Handlers")` with
> nested `Describe` per operation and `Context` per scenario. All tests use a
> mocked `DatabaseRepository`.

### TC-U005: GetHealth returns 200 with correct fields when healthy

- **Requirement:** REQ-HLT-010, REQ-HLT-020, REQ-HLT-030, REQ-HLT-050
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U007 (GetHealth uses only `startTime`, `version`, and `store.CheckHealth`)
- **Given:** A `Handler` is initialized with a known start time, version `"1.0.0"`, and a mock `DatabaseRepository` whose `CheckHealth` returns `nil`
- **When:** `GetHealth` is called on the `StrictServerInterface`
- **Then:**
  - Response is `GetHealth200JSONResponse`
  - `status` is `"healthy"`
  - `type` is `"cnpg-database-service-provider.dcm.io/health"`
  - `path` is `"health"`
  - `version` is `"1.0.0"`
  - `uptime` is an integer `>= 0`

### TC-U006: Uptime increases over time

- **Requirement:** REQ-HLT-020
- **Priority:** Medium
- **Type:** Unit
- **Given:** A `Handler` initialized with a start time 60 seconds in the past and a mock `DatabaseRepository` whose `CheckHealth` returns `nil`
- **When:** `GetHealth` is called
- **Then:** `uptime` is `>= 60`

### TC-U087: GetHealth returns "unhealthy" when health check fails

- **Requirement:** REQ-HLT-020, REQ-HLT-060
- **Priority:** High
- **Type:** Unit
- **Given:** A `Handler` is initialized with a mock `DatabaseRepository` whose `CheckHealth` returns an error
- **When:** `GetHealth` is called
- **Then:**
  - Response is `GetHealth200JSONResponse`
  - `status` is `"unhealthy"`

### TC-U088: GetHealth returns all fields when unhealthy

- **Requirement:** REQ-HLT-060
- **Priority:** High
- **Type:** Unit
- **Given:** A `Handler` initialized with version `"1.0.0"` and a mock `DatabaseRepository` whose `CheckHealth` returns an error
- **When:** `GetHealth` is called
- **Then:**
  - `status` is `"unhealthy"`
  - `type` is `"cnpg-database-service-provider.dcm.io/health"`
  - `path` is `"health"`
  - `version` is `"1.0.0"`
  - `uptime` is an integer `>= 0`

### TC-U089: CheckHealth is part of DatabaseRepository interface

- **Requirement:** REQ-HLT-070
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The `DatabaseRepository` interface includes `CheckHealth(ctx context.Context) error`
- **When:** A compile-time type assertion of the CNPG store to `DatabaseRepository` is performed
- **Then:** The assertion compiles and succeeds (covered by TC-U024)

### TC-U009: CreateDatabase returns 201 with populated read-only fields

- **Requirement:** REQ-API-020, REQ-API-060, REQ-API-070
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U008 (Handler implements StrictServerInterface — compile-time assertion)
- **Given:** A valid request body with `metadata.name="my-db"`, `spec.engine="postgresql"`, `spec.resources`
- **When:** `POST /api/v1alpha1/databases` is handled (mock repository returns success)
- **Then:**
  - HTTP status is `201`
  - Response `id` is non-empty
  - Response `path` is `"databases/{id}"`
  - Response `status` is `"PENDING"`
  - Response `create_time` is a valid timestamp close to now
  - Response `update_time` equals `create_time`
  - Response `metadata.namespace` matches the configured namespace
  - Response `connection_details` is empty string

### TC-U010: CreateDatabase generates ID when no id query param

- **Requirement:** REQ-API-030
- **Priority:** High
- **Type:** Unit
- **Given:** A valid database request body
- **When:** `POST /api/v1alpha1/databases` is called without `?id=`
- **Then:** Response `id` is a non-empty string matching the AEP-122 pattern

### TC-U011: CreateDatabase uses client-specified id

- **Requirement:** REQ-API-040
- **Priority:** High
- **Type:** Unit
- **Given:** A valid database request body
- **When:** `POST /api/v1alpha1/databases?id=my-db` is called
- **Then:** Response `id` is `"my-db"`

### TC-U012: CreateDatabase rejects invalid client IDs

- **Requirement:** REQ-API-050
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Invalid IDs that violate pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- **When:** `POST /api/v1alpha1/databases?id={invalid}` is called for each:
  - `"UPPERCASE"` (uppercase letters)
  - `"-leading-dash"` (starts with dash)
  - `"trailing-"` (ends with dash)
  - `"has_underscore"` (underscore)
  - `"a"` + 63 chars (exceeds 63-character limit)
- **Then:** Each returns HTTP `400` with RFC 9457 error body containing type `INVALID_ARGUMENT`

### TC-U013: CreateDatabase returns 409 when database with same id already exists

- **Requirement:** REQ-API-080
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U026 (Conflict error is distinguishable)
- **Given:** Mock repository returns a conflict error for a duplicate `dcm.project/dcm-instance-id`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** HTTP status is `409` AND body is RFC 9457 error with type `ALREADY_EXISTS`

### TC-U014: CreateDatabase validates request body

- **Requirement:** REQ-API-090, REQ-API-100, REQ-API-110, REQ-API-130, REQ-HTTP-090
- **Priority:** High
- **Type:** Unit (table-driven)
- **Transitively covers:** TC-U052 (invalid engine), TC-U053 (invalid metadata.name), TC-U054 (invalid memory format), TC-U055 (invalid CPU format), TC-U056 (min > max)
- **Given:** Request bodies each missing a required field or containing an invalid value
- **When:** `POST` is called for each:
  - Missing `spec` entirely
  - Missing `spec.engine`
  - Missing `spec.resources`
  - Missing `spec.resources.cpu`
  - Missing `spec.resources.cpu.min` or `.max`
  - Missing `spec.resources.memory`
  - Missing `spec.resources.memory.min` or `.max`
  - Missing `metadata`
  - Missing `metadata.name`
  - Invalid `spec.engine` value (e.g., `"mysql"`) (TC-U052)
  - Invalid `metadata.name` format (e.g., `"Invalid_Name!"`) (TC-U053)
  - Invalid memory format (e.g., `"10XB"`) (TC-U054)
  - Invalid CPU format (e.g., `"not-valid"`) (TC-U055)
  - `spec.resources.cpu.min > spec.resources.cpu.max` (TC-U056)
  - `spec.resources.memory.min > spec.resources.memory.max`
- **Then:** Each returns HTTP `400` with RFC 9457 error body containing type `INVALID_ARGUMENT`

### TC-U047: CreateDatabase accepts valid boundary IDs

- **Requirement:** REQ-API-050 (positive boundary)
- **Priority:** Medium
- **Type:** Unit (table-driven)
- **Given:** Client-specified IDs at the boundaries of the valid pattern
- **When:** `POST /api/v1alpha1/databases?id={valid}` is called for each:
  - `"a"` (single character — minimum length)
  - `"ab"` (two characters)
  - `"a"` + 62 chars of `[a-z0-9]` (exactly 63 characters — maximum length)
  - `"a-b"` (dash in middle)
  - `"a0"` (letter followed by digit)
- **Then:** Each returns HTTP `201` (mock repository returns success)

### TC-U049: CreateDatabase rejects metadata.labels colliding with DCM labels

- **Requirement:** REQ-XC-LBL-020
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Request bodies with `metadata.labels` containing reserved DCM label keys
- **When:** `POST` is called for each:
  - `metadata.labels: {"dcm.project/managed-by": "custom"}`
  - `metadata.labels: {"dcm.project/dcm-instance-id": "custom-id"}`
  - `metadata.labels: {"dcm.project/dcm-service-type": "custom-type"}`
- **Then:** Each returns HTTP `400` with RFC 9457 error body containing type `INVALID_ARGUMENT`

### TC-U015: ListDatabases returns 200 with results

- **Requirement:** REQ-API-150
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a list of 3 databases
- **When:** `GET /api/v1alpha1/databases` is called
- **Then:** HTTP status is `200` AND body has `results` array with 3 items

### TC-U016: ListDatabases supports pagination parameters

- **Requirement:** REQ-API-160
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns 10 databases and a `next_page_token` when `max_page_size=10`
- **When:** `GET /api/v1alpha1/databases?max_page_size=10` is called
- **Then:** Response has at most 10 results AND `next_page_token` is non-empty

### TC-U017: ListDatabases returns empty array when no databases exist

- **Requirement:** REQ-API-170
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns an empty list
- **When:** `GET /api/v1alpha1/databases` is called
- **Then:** HTTP status is `200` AND `results` is an empty JSON array `[]` (not `null` or absent)

### TC-U050: ListDatabases rejects invalid page_token

- **Requirement:** REQ-API-160
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns an invalid-argument error for an undecodable `page_token`
- **When:** `GET /api/v1alpha1/databases?page_token=not-a-valid-token` is called
- **Then:** HTTP status is `400` AND body is RFC 9457 error with type `INVALID_ARGUMENT`

### TC-U018: GetDatabase returns 200 for existing database

- **Requirement:** REQ-API-180
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a database for id `"abc-123"`
- **When:** `GET /api/v1alpha1/databases/abc-123` is called
- **Then:** HTTP status is `200` AND body matches the returned database

### TC-U019: GetDatabase returns 404 for non-existent database

- **Requirement:** REQ-API-190
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U025 (Not-found error is distinguishable)
- **Given:** Mock repository returns a not-found error for id `"xyz-999"`
- **When:** `GET /api/v1alpha1/databases/xyz-999` is called
- **Then:** HTTP status is `404` AND body is RFC 9457 error with type `NOT_FOUND`

### TC-U020: DeleteDatabase returns 204

- **Requirement:** REQ-API-210
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository successfully deletes database `"abc-123"`
- **When:** `DELETE /api/v1alpha1/databases/abc-123` is called
- **Then:** HTTP status is `204` AND body is empty

### TC-U021: DeleteDatabase returns 404 for non-existent database

- **Requirement:** REQ-API-220
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U025 (Not-found error is distinguishable)
- **Given:** Mock repository returns a not-found error for id `"xyz-999"`
- **When:** `DELETE /api/v1alpha1/databases/xyz-999` is called
- **Then:** HTTP status is `404` AND body is RFC 9457 error with type `NOT_FOUND`

### TC-U051: Handler returns 500 INTERNAL for unexpected store errors

- **Requirement:** REQ-API-240
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a generic (non-typed) error from any operation
- **When:** The handler processes the response
- **Then:** HTTP status is `500` AND body is RFC 9457 error with type `INTERNAL`

### TC-U022: Error responses use RFC 9457 format

- **Requirement:** REQ-API-230
- **Priority:** High
- **Type:** Unit (table-driven across error scenarios)
- **Given:** Any error condition (not found, conflict, validation failure, internal error)
- **When:** The error response is returned
- **Then:** `Content-Type` is `application/problem+json` AND body contains at minimum `type` and `title` fields

### TC-U023: Error types map to correct HTTP status codes

- **Requirement:** REQ-API-240
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Error conditions mapped as follows:

  | Error Condition      | Expected Status | Expected Type    |
  |----------------------|-----------------|------------------|
  | Invalid request body | 400             | INVALID_ARGUMENT |
  | Database not found   | 404             | NOT_FOUND        |
  | ID already exists    | 409             | ALREADY_EXISTS   |
  | Unexpected error     | 500             | INTERNAL         |

- **When:** Each error is mapped to an HTTP response
- **Then:** The status code and error type match the table

### TC-U070: ResponseErrorHandlerFunc returns RFC 9457

- **Requirement:** REQ-HTTP-091
- **Priority:** High
- **Type:** Unit
- **Given:** The strict handler adapter's `ResponseErrorHandlerFunc` is configured
- **When:** It is invoked with an error
- **Then:** The response MUST be HTTP 500 with `Content-Type: application/problem+json`
- **And** the body MUST be RFC 9457 with type `INTERNAL`
- **And** the body MUST NOT contain the raw error message

### TC-U071: Handler error responses include instance field

- **Requirement:** REQ-XC-ERR-030
- **Priority:** High
- **Type:** Unit
- **Given:** A handler error occurs
- **When:** The error response is returned
- **Then:** The `instance` field MUST be set to the request path

### TC-U081: CreateDatabase accepts provider_hints

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit
- **Given:** POST body includes `"provider_hints": {"postgres": {"storage": {"storage_class": "fast"}}}` in the spec
- **When:** `POST /api/v1alpha1/databases` is called (mock repository returns success)
- **Then:** HTTP status is `201`

### TC-U097: CreateDatabase validates spec.resources.storage pattern

- **Requirement:** REQ-API-120
- **Priority:** High
- **Type:** Unit
- **Given:** A request body with an invalid `spec.resources.storage` value (e.g., `"10XB"`)
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** HTTP status is `400` with RFC 9457 error body containing type `INVALID_ARGUMENT`

### TC-U098: CreateDatabase returns errors array for multiple validation failures

- **Requirement:** REQ-XC-ERR-050, REQ-XC-ERR-055
- **Priority:** High
- **Type:** Unit
- **Given:** A request body with two or more independent handler-level validation failures (e.g., `spec.resources.cpu.min > spec.resources.cpu.max` AND `metadata.labels` containing `dcm.project/managed-by`)
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** HTTP status is `400` with RFC 9457 error body
- **And:** The body contains an `errors` array with one entry per validation failure (at least 2)
- **And:** The top-level `detail` is a generic summary message and does NOT duplicate any individual entry

### TC-U099: CreateDatabase single validation error omits errors array

- **Requirement:** REQ-XC-ERR-060
- **Priority:** High
- **Type:** Unit
- **Given:** A request body with exactly one handler-level validation failure (e.g., `spec.resources.cpu.min > spec.resources.cpu.max`)
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** HTTP status is `400` with RFC 9457 error body
- **And:** The body contains top-level `detail` with the specific error
- **And:** The body does NOT contain an `errors` array

### TC-U100: Validation error includes pointer field for field-attributable failures

- **Requirement:** REQ-XC-ERR-070
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Handler-level validation failures attributable to specific request body fields
- **When:** `POST /api/v1alpha1/databases` is called with each failure:
  - `spec.resources.cpu.min > spec.resources.cpu.max` → expected pointer `#/spec/resources/cpu/min`
  - `spec.resources.memory.min > spec.resources.memory.max` → expected pointer `#/spec/resources/memory/min`
  - Reserved label `dcm.project/managed-by` in `metadata.labels` → expected pointer `#/metadata/labels/dcm.project~1managed-by`
- **Then:** Each response error (top-level for single-error; entry in `errors` array for multi-error) contains the expected `pointer` value
- **And:** The `/` in label keys is correctly escaped as `~1` per RFC 6901

### TC-U101: CreateDatabase rejects reserved "health" database ID

- **Requirement:** REQ-HLT-080
- **Priority:** High
- **Type:** Unit
- **Given:** A handler with a mock `DatabaseRepository`
- **When:** `POST /api/v1alpha1/databases?id=health` is called with a valid request body
- **Then:** HTTP status is `400`
- **And:** The RFC 9457 body has `type` `INVALID_ARGUMENT`
- **And:** The `detail` field mentions that `"health"` is a reserved ID
- **And:** The mock repository's `CreateDatabase` is never called

---

## 4 · Status Reconciliation Logic

> **Suggested Ginkgo structure:** `Describe("Status Reconciliation")`

### TC-U031: Pod status takes precedence — Running

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U029 (Pod phase Running → RUNNING)
- **Given:** Both a Cluster (readyInstances=0) and a Pod (phase=Running) exist for instance `"abc-123"`
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `RUNNING` (derived from Pod, not Cluster)

### TC-U032: Cluster fallback — PENDING when readyInstances=0

- **Requirement:** REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Cluster exists with `readyInstances=0` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `PENDING`

### TC-U033: Cluster fallback — FAILED when instances=0

- **Requirement:** REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Cluster exists with `instances=0` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `FAILED`

### TC-U034: PVC Lost + Pod Pending → FAILED

- **Requirement:** REQ-MON-070
- **Priority:** High
- **Type:** Unit
- **Given:** A Pod exists with phase Pending AND its associated PVC phase is Lost
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `FAILED`

### TC-U035: DELETED status when neither Cluster nor Pod exists

- **Requirement:** REQ-MON-100
- **Priority:** High
- **Type:** Unit
- **Given:** Neither Cluster nor Pod exists for a previously tracked instance
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `DELETED`

### TC-U060: Cluster deletionTimestamp → DELETING

- **Requirement:** REQ-MON-090
- **Priority:** High
- **Type:** Unit
- **Given:** A Cluster has a non-nil `deletionTimestamp`
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `DELETING`

### TC-U090: Multi-replica aggregation — any FAILED dominates

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Unit
- **Given:** 3 instances with statuses [RUNNING, RUNNING, FAILED]
- **When:** Aggregation is performed
- **Then:** Aggregate status is `FAILED`

### TC-U091: Multi-replica aggregation — any UNKNOWN (no FAILED)

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Unit
- **Given:** 3 instances with statuses [RUNNING, UNKNOWN, PENDING]
- **When:** Aggregation is performed
- **Then:** Aggregate status is `UNKNOWN`

### TC-U092: Multi-replica aggregation — any PENDING (no FAILED or UNKNOWN)

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Unit
- **Given:** 3 instances with statuses [RUNNING, RUNNING, PENDING]
- **When:** Aggregation is performed
- **Then:** Aggregate status is `PENDING`

### TC-U093: Multi-replica aggregation — all RUNNING

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Unit
- **Given:** 3 instances all with status RUNNING
- **When:** Aggregation is performed
- **Then:** Aggregate status is `RUNNING`

### TC-U102: Debouncer.Stop() is safe to call concurrently with firing timers

- **Requirement:** REQ-MON-160
- **Priority:** High
- **Type:** Unit (concurrency / race detector)
- **Given:** A debouncer with a very short window (e.g., 1ms)
- **When:** `Stop()` is called concurrently from one goroutine while multiple `Debounce()` calls are firing from N other goroutines (run with `-race`)
- **Then:** No panic occurs (no send on closed channel)
- **And:** No goroutine leak is detected after `Stop()` returns
- **And:** Any events that fired before `Stop()` completes are published; events after `Stop()` are silently dropped

### TC-U109: Multi-replica aggregate PENDING when Cluster in creating/waiting phase with a deleted replica

- **Requirement:** REQ-MON-120
- **Priority:** High
- **Type:** Unit
- **Given:** A Cluster whose CNPG phase is one of `"Creating a new replica"` or `"Waiting for the instances to become active"` AND at least one associated instance has status DELETED
- **When:** Aggregation is performed
- **Then:** The aggregate DCM status is `PENDING`

---

## Utility Test Case Index

Utility and helper functions are tested **transitively** through the
behavioural tests listed above and in the integration test plan. Each utility
TC-ID is preserved for requirements traceability but does **not** map to a
dedicated test class or `Describe` block.

---

### Structural Contracts

#### TC-U007: GetHealth only uses health check, not CRUD methods

- **Requirement:** REQ-HLT-050
- **Priority:** Medium
- **Type:** Unit (structural)
- **Given:** The `Handler` struct includes a `store` field for database CRUD and health checking
- **When:** `GetHealth` is inspected
- **Then:** It uses only `startTime`, `version`, and `store.CheckHealth` — never CRUD methods
- **Referenced by:** TC-U005

#### TC-U008: Handler implements StrictServerInterface

- **Requirement:** REQ-API-010
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The handler type exists
- **When:** A compile-time type assertion to `StrictServerInterface` is performed
- **Then:** The assertion compiles and succeeds
- **Referenced by:** TC-U009 (`var _ StrictServerInterface = (*Handler)(nil)` in test file)

#### TC-U024: DatabaseRepository interface is satisfied by CNPG store

- **Requirement:** REQ-STR-010
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The `DatabaseRepository` interface is defined with `Create`, `Get`, `List`, `Delete`, `CheckHealth`
- **When:** A compile-time type assertion of the CNPG store to `DatabaseRepository` is performed
- **Then:** The assertion compiles and succeeds
- **Referenced by:** TC-I009

#### TC-U072: StatusPublisher interface satisfied by NATS publisher

- **Requirement:** REQ-MON-145
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The StatusPublisher interface is defined
- **When:** A compile-time type assertion of the NATS publisher to StatusPublisher is performed
- **Then:** The assertion compiles and succeeds

#### TC-U108: No DCM registration code exists in the startup path

- **Requirement:** REQ-REG-010
- **Priority:** High
- **Type:** Unit (structural / static)
- **Given:** The `main` package and all packages it imports transitively
- **When:** The source is inspected
- **Then:** No call to a DCM `/providers` HTTP endpoint is present
- **And:** No DCM self-registration type or function is defined or imported
- **Note:** Implement as a `go/ast` or grep-based test that fails if the string `"/providers"` or a DCM registration client type is found in any non-test `.go` file under `cmd/` or `internal/`

---

### Error Types

#### TC-U025: Not-found error is distinguishable

- **Requirement:** REQ-STR-080
- **Priority:** High
- **Type:** Unit
- **Given:** A not-found error is created by the store error package
- **When:** The error is inspected with `errors.Is` or `errors.As`
- **Then:** It is correctly identified as a not-found error AND distinguishable from conflict and other errors
- **Referenced by:** TC-U019 (GetDatabase 404), TC-U021 (DeleteDatabase 404), TC-I032 (Get not-found integration), TC-I039 (Delete not-found integration)

#### TC-U026: Conflict error is distinguishable

- **Requirement:** REQ-STR-080
- **Priority:** High
- **Type:** Unit
- **Given:** A conflict error is created by the store error package
- **When:** The error is inspected with `errors.Is` or `errors.As`
- **Then:** It is correctly identified as a conflict error AND distinguishable from not-found errors
- **Referenced by:** TC-U013, TC-I069

---

### Validation Error Scrubbing

#### TC-U103: scrubValidationError returns the raw message for simple non-nested errors

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Unit
- **Given:** A `validation.Error` (or equivalent) whose message is a plain human-readable string with no internal paths
- **When:** `scrubValidationError` is called
- **Then:** The returned string equals the original message (no modification needed for safe errors)
- **Referenced by:** TC-U022, TC-U098, TC-U099, TC-U100

#### TC-U104: scrubValidationError strips internal file paths from error messages

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Unit
- **Given:** A validation error whose message contains an absolute file path (e.g., `/home/user/repo/internal/store/...`)
- **When:** `scrubValidationError` is called
- **Then:** The returned string does NOT contain the file path
- **And:** The returned string is a generic safe description
- **Referenced by:** TC-U022, TC-U098

#### TC-U105: scrubValidationError strips goroutine stack traces

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Unit
- **Given:** A validation error whose message contains a goroutine stack trace fragment
- **When:** `scrubValidationError` is called
- **Then:** The returned string does NOT contain stack trace lines (lines matching `goroutine`, `.go:`, or `panic:`)
- **Referenced by:** TC-U022

#### TC-U106: scrubValidationError handles nil error gracefully

- **Requirement:** REQ-XC-ERR-010
- **Priority:** Medium
- **Type:** Unit
- **Given:** `nil` is passed to `scrubValidationError`
- **When:** The function is called
- **Then:** No panic occurs AND the function returns an empty string or safe sentinel
- **Referenced by:** TC-U022

#### TC-U107: scrubValidationError is applied to all handler validation failures

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Unit (structural)
- **Given:** The handler code that constructs RFC 9457 error responses
- **When:** A validation error is turned into a response body
- **Then:** The `detail` field is always the result of `scrubValidationError(err)`, never `err.Error()` directly
- **Referenced by:** TC-U098, TC-U099, TC-U100

---

### Resource Mapping & Conversion

#### TC-U027: CPU values map to Kubernetes resource quantities

- **Requirement:** REQ-CNPG-050
- **Priority:** High
- **Type:** Unit
- **Given:** Database `spec.resources.cpu.min="500m"`, `spec.resources.cpu.max="2"`
- **When:** CPU is converted to Kubernetes resource quantities
- **Then:** `requests.cpu` is `"500m"` AND `limits.cpu` is `"2"`
- **Referenced by:** TC-I012

#### TC-U028: Memory units convert from schema format to Kubernetes format

- **Requirement:** REQ-CNPG-060
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Memory values in schema format
- **When:** Each is converted:

  | Input     | Expected Output |
  |-----------|-----------------|
  | `"1MB"`   | `"1Mi"`         |
  | `"512MB"` | `"512Mi"`       |
  | `"1GB"`   | `"1Gi"`         |
  | `"2GB"`   | `"2Gi"`         |
  | `"1TB"`   | `"1Ti"`         |
  | `"1MiB"`  | `"1Mi"`         |
  | `"1GiB"`  | `"1Gi"`         |

- **Then:** Each output matches the expected Kubernetes format
- **Referenced by:** TC-I013

#### TC-U029: Pod phase maps to DCM database status

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Pods with various phases
- **When:** Status mapping is applied:

  | Pod Phase | Expected DCM Status |
  |-----------|---------------------|
  | Pending   | PENDING             |
  | Running   | RUNNING             |
  | Failed    | FAILED              |
  | Unknown   | UNKNOWN             |

- **Then:** Each produces the correct DCM DatabaseStatus
- **Referenced by:** TC-U031, TC-I062 through TC-I064

#### TC-U094: Label map construction includes all three DCM labels

- **Requirement:** REQ-XC-LBL-010
- **Priority:** High
- **Type:** Unit
- **Given:** A database ID `"abc-123"`
- **When:** The DCM label map is constructed
- **Then:** Labels contain:
  - `dcm.project/managed-by=dcm`
  - `dcm.project/dcm-instance-id=abc-123`
  - `dcm.project/dcm-service-type=database`
- **Referenced by:** TC-I010 (resource labeling integration test)

---

### CloudEvent Construction

#### TC-U036: CloudEvent has correct v1.0 structure

- **Requirement:** REQ-MON-145, REQ-MON-150
- **Priority:** High
- **Type:** Unit
- **Given:** A status change for instance `"abc-123"` with provider name `"cnpg-sp"` and status `RUNNING`
- **When:** A CloudEvent is constructed
- **Then:**
  - `specversion` is `"1.0"`
  - `id` is a non-empty unique identifier
  - `source` is `"dcm/providers/cnpg-sp"`
  - `type` is `"dcm.status.database"`
  - `subject` is `"dcm.database"`
  - `datacontenttype` is `"application/json"`
  - `data` contains `{"id": "abc-123", "status": "RUNNING", "message": "..."}`
- **Referenced by:** TC-I047

#### TC-U037: DELETING CloudEvent published when delete initiated

- **Requirement:** REQ-MON-155, REQ-MON-150
- **Priority:** High
- **Type:** Unit
- **Given:** A delete is initiated for instance `"abc-123"`
- **When:** A DELETING status CloudEvent is constructed
- **Then:** `data.id` is `"abc-123"` AND `data.status` is `"DELETING"`
- **Referenced by:** TC-I048

#### TC-U095: FAILED CloudEvent includes failure reason in message

- **Requirement:** REQ-MON-150
- **Priority:** High
- **Type:** Unit
- **Given:** A Pod with failed container status reason `"CrashLoopBackOff"` for instance `"abc-123"`
- **When:** A FAILED status CloudEvent is constructed
- **Then:** `data.message` includes `"CrashLoopBackOff"`
- **Referenced by:** TC-I049

---

### Debounce Logic

#### TC-U038: Only last event within debounce window is published

- **Requirement:** REQ-MON-160
- **Priority:** High
- **Type:** Unit
- **Given:** A debouncer with a 2s window
- **When:** Three status changes occur within 2s: `PENDING` → `RUNNING` → `FAILED`
- **Then:** Only one event is published AND its status is `FAILED`
- **Referenced by:** TC-I066

#### TC-U039: Events after debounce window are published separately

- **Requirement:** REQ-MON-160
- **Priority:** Medium
- **Type:** Unit
- **Given:** A debouncer with a 2s window
- **When:** A status change occurs, the debounce window elapses fully, then another change occurs
- **Then:** Two separate events are published
- **Referenced by:** TC-I067

#### TC-U096: Debounce isolation — one instance does not suppress another

- **Requirement:** REQ-MON-165
- **Priority:** High
- **Type:** Unit
- **Given:** A debouncer handling two instances `"abc-123"` and `"xyz-456"`
- **When:** Rapid changes occur for `"abc-123"` while `"xyz-456"` changes once
- **Then:** `"xyz-456"`'s event MUST be published independently without delay from `"abc-123"` debouncing
- **Referenced by:** TC-I068

---

### Instance ID & Indexer

#### TC-U040: Instance ID extracted from dcm.project/dcm-instance-id label

- **Requirement:** REQ-MON-170
- **Priority:** High
- **Type:** Unit
- **Given:** A Kubernetes resource with label `dcm.project/dcm-instance-id="abc-123"`
- **When:** Instance ID extraction is performed
- **Then:** The result is `"abc-123"`
- **Referenced by:** TC-I043, TC-I044

#### TC-U041: Missing dcm.project/dcm-instance-id label handled gracefully

- **Requirement:** REQ-MON-170
- **Priority:** Medium
- **Type:** Unit
- **Given:** A Kubernetes resource without the `dcm.project/dcm-instance-id` label
- **When:** Instance ID extraction is attempted
- **Then:** An empty string or error is returned (resource is skipped, not panicked)
- **Referenced by:** TC-I042

#### TC-U042: Indexer function returns dcm.project/dcm-instance-id value

- **Requirement:** REQ-MON-030
- **Priority:** High
- **Type:** Unit
- **Given:** A Kubernetes object with label `dcm.project/dcm-instance-id="abc-123"`
- **When:** The custom indexer function is called
- **Then:** It returns `["abc-123"]`
- **Referenced by:** TC-I043

---

### OpenAPI Contract Enforcement

#### TC-U052: Invalid spec.engine value rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `spec.engine="mysql"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014

#### TC-U053: Invalid metadata.name format rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `metadata.name="Invalid_Name!"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014

#### TC-U054: Invalid memory format rejected

- **Requirement:** REQ-API-110
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `spec.resources.memory.min="10XB"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014

#### TC-U055: Invalid CPU format rejected

- **Requirement:** REQ-API-100
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `spec.resources.cpu.min="not-valid"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014

#### TC-U056: CPU min > max rejected

- **Requirement:** REQ-API-130
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `spec.resources.cpu.min="4"` and `spec.resources.cpu.max="2"`
- **When:** Validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014

#### TC-U057: max_page_size boundary enforcement

- **Requirement:** REQ-HTTP-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** Requests with out-of-range `max_page_size` values (0, -1, 1001)
- **When:** OpenAPI validation is applied
- **Then:** Each is rejected with 400 Bad Request
- **Referenced by:** TC-I008

---

## Coverage Matrix

| Requirement | Test Cases | Status |
|-------------|------------|--------|
| REQ-HTTP-050 | TC-U002, TC-U004 | Covered |
| REQ-HTTP-090 | TC-U057 (via TC-I008) | Covered |
| REQ-HTTP-091 | TC-U070 | Covered |
| REQ-HLT-010 | TC-U005 | Covered |
| REQ-HLT-020 | TC-U005, TC-U006, TC-U087, TC-U088 | Covered |
| REQ-HLT-030 | TC-U005 (transitively) | Covered |
| REQ-HLT-050 | TC-U005 | Covered |
| REQ-HLT-060 | TC-U087, TC-U088 | Covered |
| REQ-HLT-070 | TC-U089 | Covered |
| REQ-API-010 | TC-U008 (via TC-U009) | Covered |
| REQ-API-020 | TC-U009 | Covered |
| REQ-API-030 | TC-U010 | Covered |
| REQ-API-040 | TC-U011 | Covered |
| REQ-API-050 | TC-U012, TC-U047 | Covered |
| REQ-API-060 | TC-U009 | Covered |
| REQ-API-070 | TC-U009 | Covered |
| REQ-API-080 | TC-U013 | Covered |
| REQ-API-090 | TC-U014, TC-U049, TC-U052–TC-U056, TC-U081 | Covered |
| REQ-API-100 | TC-U055 (via TC-U014) | Covered |
| REQ-API-110 | TC-U054 (via TC-U014) | Covered |
| REQ-API-120 | TC-U097 | Covered |
| REQ-API-130 | TC-U056 (via TC-U014) | Covered |
| REQ-API-140 | TC-U009 | Covered |
| REQ-API-150 | TC-U015 | Covered |
| REQ-API-160 | TC-U016, TC-U050 | Covered |
| REQ-API-170 | TC-U017 | Covered |
| REQ-API-180 | TC-U018 | Covered |
| REQ-API-190 | TC-U019 | Covered |
| REQ-API-210 | TC-U020 | Covered |
| REQ-API-220 | TC-U021 | Covered |
| REQ-API-230 | TC-U022 | Covered |
| REQ-API-240 | TC-U023, TC-U051 | Covered |
| REQ-STR-010 | TC-U024 (via TC-I009) | Covered |
| REQ-STR-080 | TC-U025 (via TC-U019/U021), TC-U026 (via TC-U013) | Covered |
| REQ-CNPG-050 | TC-U027 (via TC-I012) | Covered |
| REQ-CNPG-060 | TC-U028 (via TC-I013) | Covered |
| REQ-MON-030 | TC-U042 (via TC-I043) | Covered |
| REQ-MON-060 | TC-U029, TC-U031 | Covered |
| REQ-MON-070 | TC-U034 | Covered |
| REQ-MON-080 | TC-U032, TC-U033 | Covered |
| REQ-MON-090 | TC-U060 | Covered |
| REQ-MON-100 | TC-U035 | Covered |
| REQ-MON-110 | TC-U090–TC-U093 | Covered |
| REQ-MON-120 | TC-U109 | Covered |
| REQ-MON-145 | TC-U036 (via TC-I047), TC-U072 | Covered |
| REQ-MON-150 | TC-U036 (via TC-I047), TC-U037 (via TC-I048), TC-U095 | Covered |
| REQ-MON-155 | TC-U037 | Covered |
| REQ-MON-160 | TC-U038 (via TC-I066), TC-U039 (via TC-I067), TC-U102 | Covered |
| REQ-MON-165 | TC-U096 (via TC-I068) | Covered |
| REQ-MON-170 | TC-U040 (via TC-I043/I044), TC-U041 (via TC-I042) | Covered |
| REQ-XC-ID-010 | TC-U009 (id in path), TC-U024 (via TC-I009) | Covered |
| REQ-XC-ID-020 | TC-U013 | Covered |
| REQ-XC-ERR-010 | TC-U022, TC-U103–TC-U107 | Covered |
| REQ-XC-ERR-020 | TC-U022 | Covered |
| REQ-XC-ERR-030 | TC-U071 | Covered |
| REQ-XC-ERR-040 | TC-U051, TC-I104 (integration) | Covered |
| REQ-XC-ERR-050 | TC-U098 | Covered |
| REQ-XC-ERR-055 | TC-U098 | Covered |
| REQ-HLT-080 | TC-U101 | Covered |
| REQ-XC-ERR-060 | TC-U099 | Covered |
| REQ-XC-ERR-070 | TC-U100 | Covered |
| REQ-XC-LBL-020 | TC-U049 | Covered |
| REQ-XC-CFG-010 | TC-U002, TC-U004, TC-U063 | Covered |
| REQ-XC-CFG-020 | TC-U063 | Covered |
| REQ-XC-CFG-030 | TC-U082–TC-U085 | Covered |
| REQ-REG-010 | TC-U108 | Covered |

> Requirements not listed above (REQ-HTTP-010–040, REQ-HTTP-060,
> REQ-HTTP-080, REQ-HTTP-110, REQ-HLT-040, REQ-API-200, REQ-STR-020–070,
> REQ-CNPG-010–200, REQ-MON-010–050, REQ-MON-100, REQ-MON-130,
> REQ-MON-140, REQ-MON-175, REQ-MON-180, REQ-XC-LBL-010,
> REQ-XC-LOG-010–020) are covered in the integration test plan.
> REQ-HTTP-070 (panic recovery) is covered by integration tests TC-I107–TC-I109.

---

## Notes

- **Table-driven tests:** TC-U012, TC-U014, TC-U023, TC-U028, TC-U029, TC-U047, TC-U090–U093 should be implemented as Ginkgo `DescribeTable` / `Entry` for conciseness.
- **Mock repository:** Handler tests (TC-U009–TC-U023) require a mock implementation of `DatabaseRepository`. Use Gomega's mock support or `testify/mock` wrapped for Ginkgo.
- **Compile-time checks:** TC-U008 and TC-U024 are implemented as `var _ StrictServerInterface = (*Handler)(nil)` in their respective test files. They do not need their own `It` block.
- **Time-sensitive tests:** TC-U006 depends on time. Use a clock interface or inject a time function to avoid flaky tests.
- **Utility transitive coverage:** Utility TCs (TC-U007/U008/U024–U029/U036–U042/U052–U057/U072/U094–U096/U103–U107) have no dedicated `Describe` blocks. Their coverage is achieved through the behavioural tests that reference them.
- **Multi-error validation (TC-U098–U100):** These tests require the handler to collect all independent validation errors before returning, rather than short-circuiting on the first failure. The `errors` array and `pointer` field are extensions on the RFC 9457 `Error` schema.
