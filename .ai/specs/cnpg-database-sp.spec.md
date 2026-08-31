# Specification: CNPG Database Service Provider

## 1. Overview

The CloudNativePG Database Service Provider (CNPG Database SP) is a REST API
that manages PostgreSQL databases on Kubernetes clusters using
[CloudNativePG](https://cloudnative-pg.io/) `Cluster` custom resources. It
exposes endpoints for creating, reading, and deleting databases, reports
resource status via CloudEvents over NATS, and exposes a health endpoint for
DCM control plane polling. Registration with the DCM SP Registry is handled by
the co-deployed environment agent, not by this service provider itself.

**Version scope (v1):**

- Create and delete database instances only (no update/day-2 operations)
- PostgreSQL only (`engine: postgresql`)
- Each database maps to exactly one CNPG `Cluster` resource
- Replicas configurable per database (default 1)
- Persistent storage managed by CNPG-controlled PersistentVolumeClaims
- `internal` (ClusterIP) or `external` (NodePort/LoadBalancer) network visibility
- Single configured Kubernetes namespace for all managed resources

**Non-goals (v1):**

- Day-2 operations: backup, DR, upgrade, scale, migrate
- Custom OS-level database management
- Multi-tenant database instances
- Connection pooling (pgBouncer)
- UPDATE or QUERY endpoints
- DCM self-registration (environment agent responsibility)

**Reference documents:**

- [CNPG Database SP Enhancement](https://github.com/dcm-project/enhancements/blob/main/enhancements/cloudnative-pg-database-sp/cloudnative-pg-sp.md)
- [SP Health Check](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-health-check/service-provider-health-check.md)
- [SP Status Reporting](https://github.com/dcm-project/enhancements/blob/main/enhancements/state-management/service-provider-status-reporting.md)
- [Service Type Definitions](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-type-definitions/service-type-definitions.md)
- [Environment Agent](https://github.com/dcm-project/enhancements/blob/main/enhancements/environment-agent/environment-agent.md)
- OpenAPI Spec: `api/v1alpha1/openapi.yaml` (source of truth for API contract)

---

## 2. Architecture

```
                                     +------------------+
                                     |   DCM Control    |
                                     |     Plane        |
                                     +--------+---------+
                                              |
                                 +------------+------------+
                                 |            |            |
                           Health Poll   NATS Messages  Environment
                          GET /health   (CloudEvents)    Agent
                                 |            |        (registration)
                                 v            |
+---------------------------------+           |
|    CNPG Database Service Provider           |
|                                             |
|  +-------------+  +----------------+  +----+-------------+
|  | HTTP Server |--| API Handlers   |--| Database Store   |
|  | (chi)       |  | (endpoints)    |  | (interface)      |
|  +------+------+  +----------------+  +--------+---------+
|         |                                      |
|  +------+------+                     +---------+---------+
|  | Health Svc  |                     | CNPG Store (impl) |
|  +-------------+                     +---------+---------+
|                                                |
|                              +--------+--------+--------+
|                              |        |                 |
|                     +---------+------+ +--------+  +---+--------+
|                     | Cluster        | | Pod    |  | PVC        |
|                     | Informer       | | Inform.|  | Informer   |
|                     +--------+-------+ +---+----+  +---+--------+
|                              |             |            |
|                              +------+------+------------+
|                                     |
|                              +------+------+
|                              | Status      |-----> NATS
|                              | Reconciler  |
|                              +-------------+
+-----------------------------------------------------+
                                    |
                    +---------------+---------------+
                    |  Kubernetes API               |
                    |  (CNPG Clusters, Services,    |
                    |   Pods, PVCs, Secrets)        |
                    +---------------+---------------+
                                    |
                    +---------------+---------------+
                    |  CloudNativePG Operator        |
                    |  (reconciles Clusters → Pods,  |
                    |   PVCs, Services)              |
                    +-------------------------------+
```

---

## 3. Topic Dependency Graph

| # | Topic                                  | Prefix | Depends On |
|---|----------------------------------------|--------|------------|
| 1 | HTTP Server                            | HTTP   | -          |
| 2 | Health Service                         | HLT    | 1, 4       |
| 3 | Database API Handlers                  | API    | 1, 4       |
| 4 | Database Store Interface               | STR    | -          |
| 5 | CNPG Integration                       | CNPG   | 4          |
| 6 | Resource Status Monitoring & Reporting | MON    | 5          |
| 7 | DCM Registration (environment agent)   | REG    | 1          |

```
Topic 1: HTTP Server              (independent)
Topic 4: Database Store Interface  (independent)
  |         |
  |         +---> Topic 5: CNPG Integration       (depends on 4)
  |                  |
  |                  +---> Topic 6: Status Monitoring  (depends on 5)
  |
  +---> Topic 2: Health Service         (depends on 1, 4)
  +---> Topic 3: Database API Handlers  (depends on 1, 4)
  +---> Topic 7: DCM Registration       (environment agent; SP has no code)
```

Topics 1 and 4 can be delivered in parallel. Topics 2, 3, 5, and 6 depend on
their respective prerequisites. Topic 7 is informational — the SP implements
no registration logic; it is handled externally by the environment agent.

> **Note:** Handler tests mock the `DatabaseRepository` interface; CNPG store
> tests use `client-go/kubernetes/fake` and a fake CNPG client. Registration
> is handled by the environment agent and is out of scope for this SP.

---

## 4. Topic Specifications

### 4.1 HTTP Server

#### Overview

Foundation layer: chi-based HTTP server with graceful shutdown, signal handling,
configuration loading from environment variables, and route registration for all
OpenAPI-defined endpoints. Database endpoints are under
`/api/v1alpha1/databases`, including the health endpoint at
`/api/v1alpha1/databases/health`.

Out of scope: TLS termination (handled by infrastructure/ingress),
authentication/authorization middleware, rate limiting.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HTTP-010 | The SP MUST start an HTTP server on the configured address | MUST | |
| REQ-HTTP-020 | The SP MUST register all OpenAPI-defined routes under `/api/v1alpha1/databases`, including health at `/api/v1alpha1/databases/health` | MUST | |
| REQ-HTTP-030 | The SP MUST initiate graceful shutdown on SIGTERM: stop new connections, drain in-flight requests within configured timeout, exit cleanly | MUST | |
| REQ-HTTP-040 | The SP MUST initiate graceful shutdown on SIGINT, behaving identically to REQ-HTTP-030 | MUST | |
| REQ-HTTP-050 | The SP MUST load configuration values from environment variables | MUST | |
| REQ-HTTP-060 | The SP MUST log each HTTP request at INFO level including method, path, response status code, and duration | MUST | |
| REQ-HTTP-070 | The SP MUST catch panics in HTTP handlers and return an RFC 9457 INTERNAL error response. Panics that signal intentional connection abort MUST be re-raised. If the response has already started streaming, the panic MUST be logged without writing a response body. Recovery middleware MUST be applied as the outermost middleware layer to ensure panics in any middleware are caught | MUST | |
| REQ-HTTP-080 | The SP MUST log server lifecycle events including listen address on startup | MUST | |
| REQ-HTTP-090 | The SP MUST return 400 Bad Request with RFC 9457 error body for malformed requests | MUST | |
| REQ-HTTP-091 | The API framework layer MUST return RFC 9457 error responses for request parsing and response serialization failures, not plain text | MUST | |
| REQ-HTTP-110 | The SP SHOULD enforce a configurable per-request timeout, cancelling the request context after the deadline | SHOULD | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| server.address | SP_SERVER_ADDRESS | :8080 | Listen address (host:port) |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | Graceful shutdown drain timeout |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | Per-request context timeout |

#### Acceptance Criteria

##### AC-HTTP-010: Server starts on configured address

- **Validates:** REQ-HTTP-010
- **Given** valid configuration is provided
- **When** the SP starts
- **Then** the HTTP server MUST begin listening on the configured address

##### AC-HTTP-020: Route registration

- **Validates:** REQ-HTTP-020
- **Given** the HTTP server has started
- **When** a request is made to any defined endpoint (e.g., `/api/v1alpha1/databases`, `/api/v1alpha1/databases/health`)
- **Then** the request MUST be routed to the corresponding handler

##### AC-HTTP-030: Graceful shutdown on SIGTERM

- **Validates:** REQ-HTTP-030
- **Given** the HTTP server is running
- **When** SIGTERM is received
- **Then** the server MUST stop accepting new connections
- **And** the server MUST drain in-flight requests within the configured shutdown timeout
- **And** the server MUST exit cleanly after draining or timeout

##### AC-HTTP-040: Graceful shutdown on SIGINT

- **Validates:** REQ-HTTP-040
- **Given** the HTTP server is running
- **When** SIGINT is received
- **Then** the server MUST behave identically to AC-HTTP-030

##### AC-HTTP-050: Configuration from environment variables

- **Validates:** REQ-HTTP-050
- **Given** environment variables are set (e.g., SP_SERVER_ADDRESS=:9090)
- **When** the SP starts
- **Then** the SP MUST use the values from the environment variables

##### AC-HTTP-060: Request logging

- **Validates:** REQ-HTTP-060
- **Given** any HTTP request is processed
- **When** the response is sent
- **Then** the SP MUST log at INFO level with method, path, status code, and duration

##### AC-HTTP-070: Panic recovery

- **Validates:** REQ-HTTP-070
- **Given** a handler panics during request processing
- **When** the panic is caught
- **Then** the response MUST be HTTP 500 with RFC 9457 body (type=INTERNAL)
- **And** the panic and stack trace MUST be logged at ERROR level
- **And** panics that signal intentional connection abort MUST be re-raised
- **And** if the response has already started streaming, a warning MUST be logged without writing a response body

##### AC-HTTP-080: Lifecycle logging

- **Validates:** REQ-HTTP-080
- **Given** the SP starts or stops
- **When** the server begins listening or initiates shutdown
- **Then** the SP MUST log the event including the listen address on startup

##### AC-HTTP-090: Malformed request handling

- **Validates:** REQ-HTTP-090
- **Given** a request with invalid parameters (e.g., malformed query params)
- **When** the request reaches the router
- **Then** the SP MUST return a 400 Bad Request with an RFC 9457 error body

##### AC-HTTP-091: Framework-layer error responses

- **Validates:** REQ-HTTP-091
- **Given** the API framework layer encounters a request parsing or response serialization failure
- **When** an error response is generated
- **Then** the error response MUST be RFC 9457 with `Content-Type: application/problem+json`
- **And** INTERNAL errors MUST NOT expose implementation details

##### AC-HTTP-110: Request timeout

- **Validates:** REQ-HTTP-110
- **Given** a configurable request timeout is set (default 30s)
- **When** a request exceeds the timeout
- **Then** the request context MUST be cancelled

#### Dependencies

None — independently deliverable.

---

### 4.2 Health Service

#### Overview

Implementation of `GET /api/v1alpha1/databases/health` as defined in the
OpenAPI spec. This endpoint is polled by the DCM control plane every 10 seconds
to determine SP liveness and backing infrastructure health. The endpoint checks
both Kubernetes API server reachability and CloudNativePG operator availability,
and reports `status: "healthy"` or `status: "unhealthy"` accordingly.

Out of scope: NATS connectivity checks, readiness vs liveness distinction
(future enhancement).

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HLT-010 | The SP MUST expose `GET /api/v1alpha1/databases/health` and return HTTP 200 OK | MUST | |
| REQ-HLT-020 | The health response MUST return a JSON body conforming to the Health schema with `status`, `type`, `path`, `version`, and `uptime` fields | MUST | |
| REQ-HLT-030 | The `status` field MUST be `"healthy"` when the backing K8s API server is reachable and the CNPG operator is available, or `"unhealthy"` when either check fails | MUST | DD-050 |
| REQ-HLT-040 | The response MUST set `Content-Type: application/json` | MUST | |
| REQ-HLT-050 | The health endpoint MUST be lightweight and return quickly, suitable for 10-second polling intervals. The permitted external calls are: (1) a Kubernetes API server version discovery request and (2) a check for the CNPG CRD or operator deployment | MUST | DD-050 |
| REQ-HLT-060 | When the K8s cluster is unreachable or the CNPG operator check fails, the health endpoint MUST return HTTP 200 with `status: "unhealthy"`. All other response fields (`type`, `path`, `version`, `uptime`) MUST still be populated | MUST | |
| REQ-HLT-070 | The `CheckHealth` method MUST be part of the `DatabaseRepository` interface so that the store implementation is the single source of backing-infrastructure interaction | MUST | DD-040 |
| REQ-HLT-080 | The SP MUST reject any Create request where the client-specified `id` equals `"health"` with 400 INVALID_ARGUMENT, because this ID would collide with the health endpoint path `/api/v1alpha1/databases/health` | MUST | |

#### Acceptance Criteria

##### AC-HLT-010: Health endpoint availability

- **Validates:** REQ-HLT-010
- **Given** the HTTP server is running
- **When** a GET request is made to `/api/v1alpha1/databases/health`
- **Then** the SP MUST return HTTP 200 OK

##### AC-HLT-020: Health response body — healthy

- **Validates:** REQ-HLT-020, REQ-HLT-030, REQ-HLT-050
- **Given** the SP is running, the K8s API server is reachable, and the CNPG operator is available
- **When** `GET /api/v1alpha1/databases/health` is called
- **Then** the response body MUST contain:
  - `status`: `"healthy"`
  - `type`: `"cnpg-database-service-provider.dcm.io/health"`
  - `path`: `"health"`
  - `version`: SP build version (string)
  - `uptime`: seconds since SP started (integer ≥ 0)

##### AC-HLT-025: Health response body — unhealthy

- **Validates:** REQ-HLT-020, REQ-HLT-060
- **Given** the SP is running but the K8s cluster is unreachable or CNPG operator is absent
- **When** `GET /api/v1alpha1/databases/health` is called
- **Then** the response MUST be HTTP 200 OK
- **And** the response body MUST contain `status: "unhealthy"` plus all other fields populated

##### AC-HLT-030: Health response content type

- **Validates:** REQ-HLT-040
- **Given** any call to the health endpoint
- **When** the response is returned
- **Then** the `Content-Type` header MUST be `application/json`

##### AC-HLT-080: Reserved "health" database ID rejected

- **Validates:** REQ-HLT-080
- **Given** a valid `POST /api/v1alpha1/databases?id=health` request
- **When** the handler processes the request
- **Then** the response status MUST be `400`
- **And** the body MUST be RFC 9457 Problem Details with `type` `INVALID_ARGUMENT`
- **And** the `detail` field MUST indicate the ID is reserved
- **And** no CNPG `Cluster` resource is created

#### Dependencies

Depends on Topic 1 (HTTP Server) and Topic 4 (Database Store Interface).

---

### 4.3 Database API Handlers

#### Overview

HTTP handlers for the five database endpoints defined in the OpenAPI spec.
Handlers delegate all persistence and Kubernetes operations to the
`DatabaseRepository` interface; they do not interact with Kubernetes directly.

Responses include a `services` array (populated by the store from live K8s
Service resources) and a `connection_details` field (output-only per AEP-203,
base64-encoded, empty until the database reaches RUNNING status).

**Endpoints:**
- `GET /api/v1alpha1/databases/health` — health check (Topic 4.2)
- `GET /api/v1alpha1/databases` — list databases (paginated)
- `POST /api/v1alpha1/databases` — create database
- `GET /api/v1alpha1/databases/{database_id}` — get database
- `DELETE /api/v1alpha1/databases/{database_id}` — delete database

Out of scope: authentication/authorization (401/403 responses defined in
OpenAPI for forward compatibility but not returned in v1), request body size
limits, bulk operations, UPDATE/QUERY endpoints.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-API-010 | The SP MUST implement all API operations defined in the OpenAPI specification | MUST | |
| REQ-API-020 | POST `/api/v1alpha1/databases` MUST return 201 Created with a `Database` body containing all server-populated read-only fields | MUST | |
| REQ-API-030 | When no `id` query parameter is provided, the server MUST generate an ID for the database | MUST | |
| REQ-API-040 | When an `id` query parameter is provided, the server MUST use it as the database ID | MUST | |
| REQ-API-050 | Client-specified IDs MUST be validated against the AEP-122 pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` | MUST | |
| REQ-API-060 | Newly created databases MUST have `status` set to `PENDING` in the response | MUST | |
| REQ-API-070 | The create response MUST populate all read-only fields: `id`, `path`, `status`, `create_time`, `update_time`, `metadata.namespace` | MUST | |
| REQ-API-080 | POST MUST return 409 Conflict when a database with the same `id` already exists | MUST | |
| REQ-API-090 | POST MUST validate that `spec.engine` and `spec.resources` (with `cpu` and `memory`) are present | MUST | |
| REQ-API-100 | POST MUST validate that `spec.resources.cpu.min` and `.max` match the required pattern | MUST | |
| REQ-API-110 | POST MUST validate that `spec.resources.memory.min` and `.max` match the required pattern | MUST | |
| REQ-API-120 | POST MUST validate that `spec.resources.storage` matches the required pattern when provided | MUST | |
| REQ-API-130 | POST MUST validate that `spec.resources.cpu.min` does not exceed `.max`, and `memory.min` does not exceed `.max` | MUST | SC-002 |
| REQ-API-140 | The created `Database` response `connection_details` field MUST be empty string (output-only, not yet available at creation time) | MUST | DD-150, SC-007 |
| REQ-API-150 | GET `/api/v1alpha1/databases` MUST return 200 with a `DatabaseList` body | MUST | |
| REQ-API-160 | GET `/api/v1alpha1/databases` MUST support `max_page_size` (1–1000, default 50) and `page_token` query parameters | MUST | SC-006 |
| REQ-API-170 | GET `/api/v1alpha1/databases` MUST return an empty `results` array (not null) when no databases exist | MUST | |
| REQ-API-180 | GET `/api/v1alpha1/databases/{database_id}` MUST return 200 with the `Database` body | MUST | |
| REQ-API-190 | GET `/api/v1alpha1/databases/{database_id}` MUST return 404 when the database does not exist | MUST | |
| REQ-API-200 | GET `/api/v1alpha1/databases/{database_id}` `connection_details` MUST be populated (base64-encoded) when status is RUNNING, and empty string otherwise | MUST | DD-150, SC-007 |
| REQ-API-210 | DELETE `/api/v1alpha1/databases/{database_id}` MUST return 204 No Content | MUST | |
| REQ-API-220 | DELETE `/api/v1alpha1/databases/{database_id}` MUST return 404 when the database does not exist | MUST | |
| REQ-API-230 | All error responses MUST conform to RFC 9457 with `Content-Type: application/problem+json` and at minimum `type` and `title` fields | MUST | |
| REQ-API-240 | Error types MUST map to appropriate HTTP status codes per the error mapping table | MUST | |
| REQ-API-250 | POST MUST reject requests where `metadata.labels` contains any DCM-reserved label key (`dcm.project/managed-by`, `dcm.project/dcm-instance-id`, `dcm.project/dcm-service-type`) | MUST | SC-004 |

**Error type mapping (REQ-API-240):**

| Error Condition | HTTP Status | Error Type |
|-----------------|-------------|------------|
| Invalid request body | 400 | INVALID_ARGUMENT |
| Database not found | 404 | NOT_FOUND |
| ID already exists | 409 | ALREADY_EXISTS |
| StorageClass not found | 422 | INVALID_ARGUMENT |
| Unexpected error | 500 | INTERNAL |

> **Note:** 401 and 403 responses are defined in the OpenAPI spec for forward
> compatibility but MUST NOT be returned in v1. Authentication and authorization
> are out of scope for v1.

#### Acceptance Criteria

##### AC-API-010: Create database — success

- **Validates:** REQ-API-020, REQ-API-060, REQ-API-070
- **Given** a valid Database request body with `spec.engine="postgresql"` and `spec.resources`
- **When** POST `/api/v1alpha1/databases` is called
- **Then** the response MUST be 201 Created
- **And** the body MUST be the created Database with `id`, `path`, `status=PENDING`, `create_time`, `update_time`, and `metadata.namespace` populated
- **And** `connection_details` MUST be empty string

##### AC-API-020: Create database — server-generated ID

- **Validates:** REQ-API-030
- **Given** POST `/api/v1alpha1/databases` is called without `?id=`
- **When** the database is created
- **Then** the response MUST contain a server-generated `id` matching the AEP-122 pattern

##### AC-API-030: Create database — client-specified ID

- **Validates:** REQ-API-040
- **Given** POST `/api/v1alpha1/databases?id=my-db` is called
- **When** the database is created
- **Then** the response `id` field MUST be `"my-db"`

##### AC-API-040: Create database — client ID validation

- **Validates:** REQ-API-050
- **Given** POST `/api/v1alpha1/databases?id=INVALID_ID` is called
- **When** the ID does not match pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- **Then** the response MUST be 400 Bad Request with an RFC 9457 error body

##### AC-API-050: Create database — initial status response

- **Validates:** REQ-API-060, REQ-API-140
- **Given** a valid create request that succeeds
- **When** the 201 response is returned
- **Then** the `status` field MUST be `PENDING`
- **And** `connection_details` MUST be empty string (credentials not yet available while Cluster is provisioning)

##### AC-API-060: Create database — read-only fields

- **Validates:** REQ-API-070
- **Given** a valid create request with `metadata.name="my-db"` that succeeds
- **When** the 201 response is returned
- **Then** the following server-populated read-only fields MUST be present and non-empty:
  - `id` — matches the AEP-122 pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
  - `path` — in the format `databases/{id}`
  - `create_time` — a non-zero RFC 3339 timestamp
  - `update_time` — a non-zero RFC 3339 timestamp equal to `create_time` at creation
  - `metadata.namespace` — the configured Kubernetes namespace
- **And** none of these fields MUST be settable by the client in the request body

##### AC-API-070: Create database — conflict

- **Validates:** REQ-API-080
- **Given** a database with id "existing-id" already exists
- **When** POST is called with `?id=existing-id`
- **Then** the response MUST be 409 Conflict with an RFC 9457 error body (type=ALREADY_EXISTS)
- **And** the existing resource MUST NOT be modified

##### AC-API-080: Create database — resource validation

- **Validates:** REQ-API-090, REQ-API-100, REQ-API-110, REQ-API-130
- **Given** a request body with min > max for CPU or memory
- **When** POST is called
- **Then** the response MUST be 400 Bad Request with an RFC 9457 error body (type=INVALID_ARGUMENT)

##### AC-API-090: List databases — pagination

- **Validates:** REQ-API-150, REQ-API-160
- **Given** databases exist in the store
- **When** `GET /api/v1alpha1/databases?max_page_size=2` is called
- **Then** the response is 200 with at most 2 results and `next_page_token` when more exist

##### AC-API-100: List databases — empty

- **Validates:** REQ-API-170
- **Given** no databases exist
- **When** `GET /api/v1alpha1/databases` is called
- **Then** the response MUST be 200 OK with `results: []` (not null or absent)

##### AC-API-110: Get database — success

- **Validates:** REQ-API-180
- **Given** a database with id "abc-123" exists
- **When** `GET /api/v1alpha1/databases/abc-123` is called
- **Then** the response MUST be 200 OK with the Database body

##### AC-API-120: Get database — not found

- **Validates:** REQ-API-190
- **Given** no database with id "xyz-999" exists
- **When** `GET /api/v1alpha1/databases/xyz-999` is called
- **Then** the response MUST be 404 Not Found with an RFC 9457 error body

##### AC-API-130: Get database — connection_details populated when RUNNING

- **Validates:** REQ-API-200
- **Given** a database with status RUNNING
- **When** `GET /api/v1alpha1/databases/{database_id}` is called
- **Then** `connection_details` MUST be a non-empty base64-encoded string

##### AC-API-140: Get database — connection_details empty when not RUNNING

- **Validates:** REQ-API-200
- **Given** a database with status PENDING, FAILED, or UNKNOWN
- **When** `GET /api/v1alpha1/databases/{database_id}` is called
- **Then** `connection_details` MUST be empty string

##### AC-API-150: Delete database — success

- **Validates:** REQ-API-210
- **Given** a database with id "abc-123" exists
- **When** `DELETE /api/v1alpha1/databases/abc-123` is called
- **Then** the response MUST be 204 No Content with no body

##### AC-API-160: Delete database — not found

- **Validates:** REQ-API-220
- **Given** no database with id "xyz-999" exists
- **When** `DELETE /api/v1alpha1/databases/xyz-999` is called
- **Then** the response MUST be 404 Not Found with an RFC 9457 error body

##### AC-API-170: Error response format

- **Validates:** REQ-API-230
- **Given** any error condition
- **When** an error response is returned
- **Then** the response MUST have `Content-Type: application/problem+json`
- **And** the body MUST contain at minimum `type` and `title` fields

##### AC-API-180: List databases — invalid page_token

- **Validates:** REQ-API-160, SC-006
- **Given** a `page_token` value that is not a valid opaque token issued by a prior List response
- **When** `GET /api/v1alpha1/databases?page_token=INVALID` is called
- **Then** the response MUST be 400 Bad Request with an RFC 9457 error body (type=INVALID_ARGUMENT)

##### AC-API-190: Create database — DCM label collision rejected

- **Validates:** REQ-API-250, SC-004
- **Given** a create request with `metadata.labels` containing `dcm.project/managed-by=custom`
- **When** POST `/api/v1alpha1/databases` is called
- **Then** the response MUST be 400 Bad Request with an RFC 9457 error body
- **And** no Kubernetes resources MUST be created

##### AC-API-200: List databases — max_page_size out of range

- **Validates:** REQ-API-160
- **Given** `?max_page_size=0` or `?max_page_size=1001` is supplied
- **When** `GET /api/v1alpha1/databases` is called
- **Then** the response MUST be 400 Bad Request with an RFC 9457 error body (type=INVALID_ARGUMENT)

#### Dependencies

Depends on Topic 1 (HTTP Server) and Topic 4 (Database Store Interface).

---

### 4.4 Database Store Interface

#### Overview

The `DatabaseRepository` interface decouples API handlers from the CNPG/K8s
implementation. Handlers depend only on this interface; all Kubernetes and
CloudNativePG interactions live in the CNPG store implementation (Topic 4.5).

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-STR-010 | The SP MUST define a `DatabaseRepository` interface with `Create`, `Get`, `List`, `Delete`, and `CheckHealth` methods | MUST | DD-040 |
| REQ-STR-020 | `Create` MUST return the created `Database` with all server-populated read-only fields (`id`, `path`, `status`, `create_time`, `update_time`, `metadata.namespace`) | MUST | |
| REQ-STR-030 | `Create` MUST return a conflict error if a database with the same `id` already exists | MUST | SC-001 |
| REQ-STR-040 | `Get` MUST return the matching `Database` for a valid `database_id`, or a not-found error if no match exists | MUST | |
| REQ-STR-050 | `List` MUST accept pagination parameters (`max_page_size`, `page_token`) and return a paginated `DatabaseList` | MUST | |
| REQ-STR-060 | `List` MUST default to `max_page_size=50` when not specified | MUST | |
| REQ-STR-070 | `Delete` MUST remove the database matching the `database_id`, or return a not-found error if no match | MUST | |
| REQ-STR-080 | The store MUST define typed sentinel errors for not-found and conflict conditions to enable handlers to map them to correct HTTP status codes | MUST | |

#### Acceptance Criteria

##### AC-STR-010: Create operation populates read-only fields

- **Validates:** REQ-STR-020
- **Given** a valid `Database` is passed to `Create`
- **When** the operation succeeds
- **Then** the returned `Database` MUST have all read-only fields populated

##### AC-STR-020: Create conflict detection

- **Validates:** REQ-STR-030
- **Given** a database with `dcm.project/dcm-instance-id` "existing-id" already exists
- **When** `Create` is called with the same id
- **Then** a conflict error MUST be returned

##### AC-STR-030: Get — not found

- **Validates:** REQ-STR-040
- **Given** no database with id "xyz-999" exists
- **When** `Get("xyz-999")` is called
- **Then** a not-found error MUST be returned

##### AC-STR-050: List pagination

- **Validates:** REQ-STR-050
- **Given** 75 databases exist and max_page_size is 50
- **When** `List` is called
- **Then** the first page MUST contain 50 databases and `next_page_token` MUST be non-empty

##### AC-STR-080: Error type — not-found is distinguishable

- **Validates:** REQ-STR-080
- **Given** a not-found error is returned by the store
- **When** the error is inspected with `errors.Is` or `errors.As`
- **Then** it MUST be distinguishable as a not-found error

#### Dependencies

None — independently deliverable.

---

### 4.5 CNPG Integration

#### Overview

The CNPG store is the concrete implementation of `DatabaseRepository`. It maps
`Database` resources to CNPG `Cluster` Kubernetes custom resources. Each
database is backed by exactly one `Cluster` in the configured namespace.

**Resource identity:** The database ID (`dcm-instance-id`) is stored as the
label `dcm.project/dcm-instance-id` on all created K8s resources. The
Kubernetes resource name is server-assigned via `generateName` using
`metadata.name` as the prefix. Resources are always looked up by label, not by
name, so name uniqueness is not required.

**Service configuration:** CloudNativePG creates three services by default (rw,
ro, r). The SP disables `ro` and `r`, keeping only the `rw` ClusterIP service
which cannot be disabled. When `network.visibility=external`, an additional
service of the configured external type (LoadBalancer or NodePort) is created.

**Credential management:** If `provider_hints.postgres.initdb.password` is
specified, the SP creates a `kubernetes.io/basic-auth` Secret carrying the
user's password before creating the `Cluster`. This Secret is labelled with
DCM labels and is explicitly deleted during the delete operation.

CloudNativePG manages the lifecycle of associated Pods, PVCs, and Services
automatically once the `Cluster` is created or deleted.

Out of scope: day-2 Cluster resizing, CNPG backup/restore configuration,
custom PostgreSQL parameters, multiple databases per Cluster, connection pooling.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-CNPG-010 | The CNPG store MUST create a CNPG `Cluster` resource for each `Database` in the configured namespace | MUST | DD-010, DD-030 |
| REQ-CNPG-020 | The Kubernetes resource name for the `Cluster` MUST be server-assigned via `generateName` using `metadata.name` as prefix (e.g., `"my-db-"`) | MUST | DD-080 |
| REQ-CNPG-030 | Resources MUST be looked up by the `dcm.project/dcm-instance-id` label, not by name | MUST | DD-080, SC-001 |
| REQ-CNPG-040 | `Cluster.spec.instances` MUST be set from `spec.replicas` (default 1 when not provided) | MUST | |
| REQ-CNPG-050 | CPU `min` MUST map to the Kubernetes container resource `requests.cpu`; `max` MUST map to `limits.cpu` | MUST | |
| REQ-CNPG-060 | Memory `min` MUST map to the Kubernetes container resource `requests.memory`; `max` MUST map to `limits.memory`. Memory values MUST be converted from schema format (MB/GB/TB) to Kubernetes format (Mi/Gi/Ti) | MUST | DD-090 |
| REQ-CNPG-070 | `spec.resources.storage` MUST map to `Cluster.spec.storage.size` | MUST | |
| REQ-CNPG-080 | `spec.version` MUST map to the PostgreSQL major version for CNPG image selection; when not provided, the SP MUST use the configured default version | MUST | |
| REQ-CNPG-090 | The SP MUST disable the CNPG-default `ro` and `r` services; only the `rw` service is created | MUST | DD-060 |
| REQ-CNPG-100 | When `network.visibility=internal` (or not specified), only the default `rw` ClusterIP service MUST be active | MUST | DD-060 |
| REQ-CNPG-110 | When `network.visibility=external`, an additional service of the configured `external_service_type` (LoadBalancer or NodePort) MUST be created | MUST | DD-070 |
| REQ-CNPG-120 | When `network.port` is specified, it MUST be propagated to the Cluster port configuration | MUST | |
| REQ-CNPG-130 | If `provider_hints.postgres.storage.storage_class` is provided, it MUST be used as the `storageClass` for the CNPG Cluster's PVC | MUST | |
| REQ-CNPG-140 | If `provider_hints.postgres.initdb` is provided, the SP MUST configure `Cluster.spec.bootstrap.initdb` accordingly; `database`, `user`, and password reference MUST be set | MUST | DD-160, SC-005 |
| REQ-CNPG-150 | If `provider_hints.postgres.initdb.password` is provided, the SP MUST create a `kubernetes.io/basic-auth` Secret before creating the Cluster, and reference it in the bootstrap config | MUST | DD-160 |
| REQ-CNPG-160 | If `provider_hints.postgres.initdb` is not provided, CNPG bootstrap defaults apply (database=app, user=app, randomly generated password) | MUST | SC-005 |
| REQ-CNPG-170 | `Delete` MUST delete the `Cluster` resource (CNPG cascades Pods, PVCs, and its managed Services) | MUST | |
| REQ-CNPG-180 | `Delete` MUST also explicitly delete all `Secrets` labelled with `dcm.project/dcm-instance-id` matching the database ID. The operation MUST succeed even if no such Secrets exist | MUST | SC-008 |
| REQ-CNPG-190 | The store MUST return a conflict error when a Cluster with the same `dcm.project/dcm-instance-id` label already exists in the namespace | MUST | SC-001 |
| REQ-CNPG-200 | If an `image_catalog` is configured, it MUST be set on the Cluster resource for PostgreSQL image resolution | SHOULD | |

**Memory unit conversion (REQ-CNPG-060):**

| Schema Format | Kubernetes Format |
|---------------|-------------------|
| MB | Mi |
| GB | Gi |
| TB | Ti |
| MiB | Mi |
| GiB | Gi |
| TiB | Ti |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| kubernetes.namespace | SP_K8S_NAMESPACE | default | Namespace for all managed resources |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (auto) | Path to kubeconfig (empty = in-cluster) |
| cnpg.externalServiceType | SP_CNPG_EXTERNAL_SERVICE_TYPE | - (required) | Service type for `external` visibility (LoadBalancer or NodePort) |
| cnpg.defaultVersion | SP_CNPG_DEFAULT_VERSION | 18 | Default PostgreSQL major version |
| cnpg.defaultStorageClass | SP_CNPG_DEFAULT_STORAGE_CLASS | (optional) | Default storage class for PVCs |
| cnpg.imageCatalog | SP_CNPG_IMAGE_CATALOG | (optional) | ImageCatalog resource name for PostgreSQL images |

#### Acceptance Criteria

##### AC-CNPG-010: Cluster created for new database

- **Validates:** REQ-CNPG-010, REQ-CNPG-020, REQ-CNPG-030
- **Given** a valid `CreateDatabase` request with `metadata.name="my-db"`
- **When** `Create` is called
- **Then** a `Cluster` with `generateName` prefix `"my-db-"` MUST exist in the configured namespace
- **And** it MUST carry the `dcm.project/dcm-instance-id` label matching the database ID

##### AC-CNPG-020: Storage mapped to Cluster PVC size

- **Validates:** REQ-CNPG-070
- **Given** `spec.resources.storage="10GB"` in the create request
- **When** the `Cluster` is created
- **Then** `Cluster.spec.storage.size` MUST be `"10Gi"`

##### AC-CNPG-025: Version mapped to PostgreSQL major version

- **Validates:** REQ-CNPG-080
- **Given** `spec.version="16"` in the create request
- **When** the `Cluster` is created
- **Then** the CNPG image version selector MUST use major version `16`
- **And** when `spec.version` is absent, the configured default version MUST be used

##### AC-CNPG-030: Labels on all created resources

- **Validates:** REQ-XC-LBL-010
- **Given** any resource (Cluster, Service, Secret) is created for database `"abc-123"`
- **When** the resource is applied
- **Then** it MUST carry labels:
  - `dcm.project/managed-by=dcm`
  - `dcm.project/dcm-instance-id=abc-123`
  - `dcm.project/dcm-service-type=database`

##### AC-CNPG-035: Port propagated to Cluster

- **Validates:** REQ-CNPG-120
- **Given** `network.port=5433` in the create request
- **When** the `Cluster` is created
- **Then** the Cluster port configuration MUST include port `5433`

##### AC-CNPG-040: Replicas default to 1

- **Validates:** REQ-CNPG-040
- **Given** `spec.replicas` is not set in the request
- **When** the `Cluster` is created
- **Then** `Cluster.spec.instances` MUST be `1`

##### AC-CNPG-045: StorageClass applied when provided

- **Validates:** REQ-CNPG-130
- **Given** `provider_hints.postgres.storage.storage_class="fast-ssd"` in the create request
- **When** the `Cluster` is created
- **Then** `Cluster.spec.storage.storageClass` MUST be `"fast-ssd"`

##### AC-CNPG-050: CPU resource mapping

- **Validates:** REQ-CNPG-050
- **Given** `spec.resources.cpu.min="500m"` and `spec.resources.cpu.max="2"`
- **When** the `Cluster` is created
- **Then** the instance container MUST have `requests.cpu="500m"` and `limits.cpu="2"`

##### AC-CNPG-055: initdb bootstrap applied without password

- **Validates:** REQ-CNPG-140
- **Given** `provider_hints.postgres.initdb.database="mydb"` and `initdb.user="owner"` without a password
- **When** the `Cluster` is created
- **Then** `Cluster.spec.bootstrap.initdb.database` MUST be `"mydb"` and `.user` MUST be `"owner"`
- **And** no Secret MUST be created

##### AC-CNPG-060: Memory resource mapping with unit conversion

- **Validates:** REQ-CNPG-060
- **Given** `spec.resources.memory.min="1GB"` and `spec.resources.memory.max="2GB"`
- **When** the `Cluster` is created
- **Then** `requests.memory="1Gi"` and `limits.memory="2Gi"`

##### AC-CNPG-065: Delete succeeds when no Secrets exist

- **Validates:** REQ-CNPG-180, SC-008
- **Given** database "abc-123" has no associated Secrets (no initdb password was provided)
- **When** `Delete` is called for "abc-123"
- **Then** the operation MUST succeed and the Cluster MUST be removed
- **And** the absence of Secrets MUST NOT cause an error

##### AC-CNPG-075: Image catalog applied when configured

- **Validates:** REQ-CNPG-200
- **Given** SP is configured with `SP_CNPG_IMAGE_CATALOG=my-catalog`
- **When** a `Cluster` is created
- **Then** the Cluster resource MUST reference image catalog `"my-catalog"`

##### AC-CNPG-085: Replicas explicitly set in request

- **Validates:** REQ-CNPG-040
- **Given** `spec.replicas=3` in the create request
- **When** the `Cluster` is created
- **Then** `Cluster.spec.instances` MUST be `3`

##### AC-CNPG-090: Only rw service created for internal visibility

- **Validates:** REQ-CNPG-090, REQ-CNPG-100
- **Given** `network.visibility=internal` (or absent)
- **When** the `Cluster` is created
- **Then** only the default `rw` ClusterIP service MUST exist; `ro` and `r` services MUST be disabled

##### AC-CNPG-095: Duplicate DCM label on existing resource → conflict

- **Validates:** REQ-CNPG-190, SC-001
- **Given** a Cluster already exists with `dcm.project/dcm-instance-id=abc-123` but a different `metadata.name`
- **When** `Create` is called with id `"abc-123"`
- **Then** a conflict error MUST be returned and the existing Cluster MUST NOT be modified

##### AC-CNPG-110: External service created for external visibility

- **Validates:** REQ-CNPG-110
- **Given** `network.visibility=external` and SP configured with `externalServiceType=LoadBalancer`
- **When** the `Cluster` is created
- **Then** an additional LoadBalancer service MUST be created alongside the `rw` ClusterIP service

##### AC-CNPG-150: Password secret created when initdb password provided

- **Validates:** REQ-CNPG-150
- **Given** `provider_hints.postgres.initdb.password="secret123"`
- **When** the `Cluster` is created
- **Then** a `kubernetes.io/basic-auth` Secret carrying the password MUST exist with DCM labels
- **And** the `Cluster` bootstrap config MUST reference it

##### AC-CNPG-170: Delete removes Cluster

- **Validates:** REQ-CNPG-170
- **Given** a database with id "abc-123" exists
- **When** `Delete` is called
- **Then** the corresponding `Cluster` MUST be removed from Kubernetes

##### AC-CNPG-180: Delete also removes DCM-owned Secrets

- **Validates:** REQ-CNPG-180
- **Given** a password Secret labelled `dcm.project/dcm-instance-id=abc-123` exists
- **When** `Delete` is called for database "abc-123"
- **Then** the Secret MUST also be deleted

##### AC-CNPG-190: Conflict detection on duplicate ID

- **Validates:** REQ-CNPG-190
- **Given** a Cluster with label `dcm.project/dcm-instance-id=abc-123` already exists
- **When** `Create` is called with id "abc-123"
- **Then** a conflict error MUST be returned and the existing Cluster MUST NOT be modified

#### Dependencies

Depends on Topic 4 (Database Store Interface).

---

### 4.6 Resource Status Monitoring & Reporting

#### Overview

The status monitor watches CNPG `Cluster`, `Pod`, and `PersistentVolumeClaim`
resources using three `SharedIndexInformer` instances and reconciles their
states into the DCM `DatabaseStatus` enum. Status changes are published as
CloudEvents v1.0 on the NATS subject `dcm.database`.

The layered monitoring approach provides complete visibility: Pod status (actual
runtime), PVC status (storage failures), and Cluster status (desired state /
replica failures) are combined by a precedence-ordered reconciliation logic.
A debounce mechanism prevents rapid oscillations from flooding the messaging
system.

**Note on DELETING status:** The CNPG Database SP introduces a `DELETING`
intermediate status, published via CloudEvent when a delete is initiated. This
status is not present in the current `DatabaseStatus` enum in `openapi.yaml`
and MUST be added before this topic is implemented.

Out of scope: JetStream stream management (consumer's responsibility), event
replay/history, per-instance NATS subjects.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-MON-010 | The SP MUST maintain three `SharedIndexInformer` instances watching `Cluster`, `Pod`, and `PersistentVolumeClaim` resources in the configured namespace | MUST | DD-100 |
| REQ-MON-020 | All three informers MUST filter using label selector `dcm.project/managed-by=dcm,dcm.project/dcm-service-type=database` | MUST | DD-130 |
| REQ-MON-030 | All three informers MUST maintain a secondary index on the `dcm.project/dcm-instance-id` label to enable fast lookup by database ID | MUST | |
| REQ-MON-040 | Pod informers MUST additionally filter using `cnpg.io/podRole=instance` to exclude non-PostgreSQL pods (e.g., backup, pgBouncer) | MUST | SC-009 |
| REQ-MON-050 | When any informer receives an event, the SP MUST reconcile status from all three resource types using the precedence rules defined in this section | MUST | |
| REQ-MON-060 | Pod status MUST take highest precedence: Running→RUNNING, Pending→PENDING, Failed→FAILED, Unknown→UNKNOWN | MUST | |
| REQ-MON-070 | When Pod is Pending and the corresponding PVC phase is Lost, status MUST be FAILED (PVC failure takes precedence over Pod Pending) | MUST | |
| REQ-MON-080 | When no Pod exists, Cluster status MUST be used: `readyInstances=0` → PENDING; `instances=0` → FAILED | MUST | |
| REQ-MON-090 | When the Cluster resource has a `deletionTimestamp` set, status MUST be DELETING | MUST | DD-020, DD-110 |
| REQ-MON-100 | When neither Cluster nor any Pod exists for a previously tracked instance, status MUST be DELETED | MUST | DD-110 |
| REQ-MON-110 | For multi-replica databases, per-instance statuses MUST be aggregated: any FAILED → FAILED; any UNKNOWN (no FAILED) → UNKNOWN; any PENDING (no FAILED or UNKNOWN) → PENDING; all RUNNING → RUNNING | MUST | |
| REQ-MON-120 | When Cluster phase is `Creating a new replica` or `Waiting for the instances to become active` and any replica is DELETED, aggregate status MUST be PENDING | MUST | |
| REQ-MON-130 | When all instances are DELETED and the Cluster resource is not found, status MUST be DELETED | MUST | DD-110 |
| REQ-MON-140 | Status changes MUST be published as CloudEvents v1.0 on NATS subject `dcm.database` | MUST | |
| REQ-MON-145 | CloudEvent attributes MUST be set as: `source=dcm/providers/{provider_name}`, `type=dcm.status.database`, `subject=dcm.database`, `datacontenttype=application/json` | MUST | DD-120 |
| REQ-MON-150 | The CloudEvent data payload MUST include `id` (DCM instance ID), `status` (DCM status string), and `message` (human-readable description) | MUST | DD-120 |
| REQ-MON-155 | When `DELETE` is called on a database, the SP MUST publish a DELETING CloudEvent before initiating Cluster deletion | MUST | DD-020, DD-110 |
| REQ-MON-160 | Status events MUST be debounced per instance to avoid flooding the messaging system during rapid status oscillation | MUST | |
| REQ-MON-165 | Debounce MUST be applied independently per database instance; rapid changes for one instance MUST NOT suppress events for another | MUST | |
| REQ-MON-170 | The instance ID MUST be extracted from the `dcm.project/dcm-instance-id` label on the resource | MUST | DD-130 |
| REQ-MON-175 | Resource informers MUST start as asynchronous background tasks after the HTTP server is ready, and stop cleanly on graceful shutdown | MUST | |
| REQ-MON-180 | NATS connection failure MUST be logged at ERROR level; the SP MUST continue operating and retry publishing. The NATS connection MUST use unlimited reconnection with disconnect/reconnect event logging | MUST | DD-140 |
| REQ-MON-185 | After the informer cache sync completes on startup, the SP MUST publish a CloudEvent for every existing managed CNPG Cluster, reflecting its current reconciled status, so that DCM is synchronised with databases that existed before the SP started | MUST | |

**Status mapping — single instance (REQ-MON-050 through REQ-MON-100):**

| DCM Status | Primary Source | Kubernetes Condition | Precedence |
|------------|----------------|----------------------|------------|
| PENDING | Pod | Pod.Phase = Pending (PVC not Lost) | 1 |
| RUNNING | Pod | Pod.Phase = Running | 1 |
| FAILED | Pod | Pod.Phase = Failed | 1 |
| UNKNOWN | Pod | Pod.Phase = Unknown (node lost) | 1 |
| FAILED | PVC | Pod.Phase = Pending AND PVC.Phase = Lost | 1 |
| PENDING | Cluster | readyInstances = 0 AND no Pod exists | 2 |
| FAILED | Cluster | instances = 0 AND no Pod exists | 2 |
| DELETING | Cluster | Cluster has deletionTimestamp set | 2 |
| DELETED | All | Neither Cluster nor Pod found | 3 |

**Aggregate status — multi-instance (REQ-MON-110 through REQ-MON-130):**

| DCM Status | Condition | Precedence |
|------------|-----------|------------|
| FAILED | Any instance FAILED | 1 |
| UNKNOWN | Any instance UNKNOWN (no FAILED) | 1 |
| PENDING | Any instance PENDING (no FAILED or UNKNOWN) | 1 |
| RUNNING | All instances RUNNING | 1 |
| DELETING | Cluster has deletionTimestamp set | 1 |
| PENDING | Cluster phase = creating/waiting AND any replica DELETED | 2 |
| DELETED | All instances DELETED AND Cluster not found | 3 |

> **Note:** SUCCEEDED status is intentionally excluded. CNPG Clusters are
> long-running services; SUCCEEDED only applies to resource types with a
> defined completion state (e.g., Jobs).

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| nats.url | SP_NATS_URL | (required) | NATS server URL |
| provider.name | SP_NAME | (required) | Provider name for CloudEvents `source` field |
| nats.subject | SP_NATS_SUBJECT | dcm.database | NATS subject for status events |
| monitoring.debounceWindow | SP_MONITOR_DEBOUNCE_WINDOW | 2s | Debounce window per instance |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | Informer cache resync period |

#### Acceptance Criteria

##### AC-MON-010: Three informers started

- **Validates:** REQ-MON-010
- **Given** the SP starts with valid K8s credentials
- **When** the monitoring subsystem initializes
- **Then** three `SharedIndexInformer` instances MUST be created for Cluster, Pod, and PVC

##### AC-MON-020: Label selector filtering

- **Validates:** REQ-MON-020
- **Given** the resource informers are running
- **When** resources are watched
- **Then** only resources carrying both `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=database` MUST be observed

##### AC-MON-045: Pod informer filters by cnpg.io/podRole=instance

- **Validates:** REQ-MON-040, SC-009
- **Given** a Pod with `cnpg.io/podRole=backup` exists in the namespace alongside an instance Pod
- **When** the Pod informer fires an event
- **Then** only the Pod with `cnpg.io/podRole=instance` MUST trigger status reconciliation
- **And** backup, pgBouncer, and other non-instance Pods MUST be ignored

##### AC-MON-055: Cluster readyInstances=0 no Pod → PENDING

- **Validates:** REQ-MON-080
- **Given** a Cluster exists with `readyInstances=0` and no Pod exists for the database
- **When** the Cluster informer fires an event
- **Then** a PENDING CloudEvent MUST be published

##### AC-MON-060: Pod Running → RUNNING

- **Validates:** REQ-MON-060
- **Given** a Pod for database "abc-123" transitions to phase Running
- **When** the Pod informer fires an update event
- **Then** a RUNNING CloudEvent MUST be published on `dcm.database`

##### AC-MON-065: Cluster instances=0 no Pod → FAILED

- **Validates:** REQ-MON-080
- **Given** a Cluster exists with `instances=0` and no Pod exists for the database
- **When** the Cluster informer fires an event
- **Then** a FAILED CloudEvent MUST be published

##### AC-MON-070: Pod Pending + PVC Lost → FAILED

- **Validates:** REQ-MON-070
- **Given** a Pod is in phase Pending and its PVC phase is Lost
- **When** the PVC informer fires an update event
- **Then** a FAILED CloudEvent MUST be published

##### AC-MON-075: All replicas RUNNING → RUNNING aggregate

- **Validates:** REQ-MON-110
- **Given** a 3-replica database has all 3 Pods in phase Running
- **When** the status is reconciled
- **Then** the aggregate status MUST be RUNNING

##### AC-MON-085: DELETING CloudEvent published before Cluster deletion

- **Validates:** REQ-MON-155, DD-020, DD-110
- **Given** `DELETE /api/v1alpha1/databases/abc-123` is called
- **When** the delete handler processes the request
- **Then** a DELETING CloudEvent MUST be published on `dcm.database` BEFORE the Cluster is deleted from Kubernetes
- **And** the CloudEvent `data.status` MUST be `"DELETING"`

##### AC-MON-090: Cluster deletionTimestamp → DELETING

- **Validates:** REQ-MON-090, REQ-MON-155
- **Given** a delete is initiated for database "abc-123"
- **When** the SP sets a deletionTimestamp on the Cluster (or receives the Cluster update event)
- **Then** a DELETING CloudEvent MUST be published

##### AC-MON-100: Neither Cluster nor Pod → DELETED

- **Validates:** REQ-MON-100
- **Given** database "abc-123" was previously tracked
- **And** both Cluster and Pods have been removed from Kubernetes
- **When** the deletion event is reconciled
- **Then** a DELETED CloudEvent MUST be published

##### AC-MON-110: Multi-replica aggregation — any FAILED

- **Validates:** REQ-MON-110
- **Given** a 3-replica database has 2 Running and 1 Failed Pod
- **When** the status is reconciled
- **Then** the aggregate status MUST be FAILED

##### AC-MON-120: Debounce is per-instance, not global

- **Validates:** REQ-MON-165
- **Given** instance "abc-123" is rapidly oscillating status within the debounce window
- **And** instance "xyz-456" emits a single distinct status change
- **When** both events are processed concurrently
- **Then** the status change for "xyz-456" MUST be published immediately
- **And** the debounce for "abc-123" MUST NOT delay or suppress events for "xyz-456"

##### AC-MON-140: CloudEvents format

- **Validates:** REQ-MON-140, REQ-MON-145, REQ-MON-150
- **Given** a status change for instance "abc-123" with provider name "cnpg-sp"
- **When** the event is published
- **Then** the CloudEvent MUST include:
  - `specversion`: `"1.0"`
  - `id`: unique event identifier
  - `source`: `"dcm/providers/cnpg-sp"`
  - `type`: `"dcm.status.database"`
  - `subject`: `"dcm.database"`
  - `datacontenttype`: `"application/json"`
  - `data`: `{"id": "abc-123", "status": "<DCM_STATUS>", "message": "<description>"}`

##### AC-MON-145: Informers start after HTTP server is ready

- **Validates:** REQ-MON-175
- **Given** the SP is starting
- **When** the HTTP server begins accepting connections
- **Then** the three informers MUST be started as background goroutines after the server is ready
- **And** informer startup MUST NOT block or delay HTTP request handling

##### AC-MON-155: Informers stop on graceful shutdown

- **Validates:** REQ-MON-175
- **Given** the SP receives SIGTERM or SIGINT
- **When** graceful shutdown begins
- **Then** all three informers MUST stop cleanly
- **And** in-flight reconciliation events MUST be allowed to complete or be cancelled via context

##### AC-MON-160: Debounce suppresses duplicate events

- **Validates:** REQ-MON-160
- **Given** multiple consecutive identical status events occur within the debounce window for instance "abc-123"
- **When** the monitor processes them
- **Then** only one CloudEvent MUST be published for that window

##### AC-MON-165: NATS reconnects after disconnect

- **Validates:** REQ-MON-180, DD-140
- **Given** the NATS server becomes temporarily unavailable
- **When** the NATS client detects the disconnect
- **Then** the disconnect MUST be logged at ERROR level
- **And** the client MUST attempt reconnection with unlimited retries
- **And** reconnection events MUST be logged
- **And** the SP MUST continue serving HTTP requests during the NATS outage

##### AC-MON-175: CloudEvent data includes message field

- **Validates:** REQ-MON-150
- **Given** any status CloudEvent is published
- **When** the event data payload is inspected
- **Then** the `data` object MUST include a non-empty `message` field providing a human-readable description of the status

##### AC-MON-185: Secondary index on dcm-instance-id enables fast lookup

- **Validates:** REQ-MON-030
- **Given** an informer event arrives for a Pod with `dcm.project/dcm-instance-id=abc-123`
- **When** the reconciler retrieves all resources for that instance
- **Then** the lookup MUST use the secondary index on `dcm.project/dcm-instance-id` rather than listing all resources and filtering in memory

##### AC-MON-186: Initial status sync on startup

- **Validates:** REQ-MON-185
- **Given** the SP starts with N existing managed CNPG Clusters in the namespace
- **When** the informer cache sync completes
- **Then** exactly N CloudEvents MUST be published to NATS, one per Cluster, each reflecting its current reconciled DCM status
- **And** these events MUST be published before any new informer events are processed

#### Dependencies

Depends on Topic 5 (CNPG Integration).

---

### 4.7 DCM Registration

#### Overview

Registration of this SP with the DCM control plane is **not** the
responsibility of this service provider. The environment agent registers the
CNPG Database SP on its behalf during environment bootstrapping. The SP itself
contains no registration code, no registration retry logic, and no
`/providers` API call.

This is a deliberate architectural difference from the K8s Container SP, which
self-registers. Here the environment agent owns the SP registration lifecycle:
it supplies the correct endpoint, service type, and operations list to DCM, and
is responsible for re-registration if the SP is redeployed.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-REG-010 | The SP MUST NOT contain any DCM registration code or startup registration logic | MUST | Contrast with k8s-container-sp |
| REQ-REG-020 | The SP MUST expose a stable endpoint URL that the environment agent can register on its behalf | MUST | |

#### Acceptance Criteria

##### AC-REG-010: No self-registration on startup

- **Validates:** REQ-REG-010
- **Given** the SP starts
- **When** startup completes
- **Then** no outbound registration request MUST have been made to any DCM control plane endpoint
- **And** the SP MUST serve requests immediately without waiting for any registration acknowledgement

##### AC-REG-020: Stable endpoint for environment agent

- **Validates:** REQ-REG-020
- **Given** the SP is running at the configured server address
- **When** the environment agent registers it with DCM
- **Then** the SP endpoint MUST be reachable at the configured address for the lifetime of the SP process

#### Dependencies

Depends on Topic 1 (HTTP Server).

---

## 5. Cross-Cutting Concerns

### 5.1 Resource Identity

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ID-010 | Two identifiers MUST be used for database resources: `id` (DCM identifier, used in URL paths and stored as `dcm.project/dcm-instance-id` label) and `metadata.name` (used as the `generateName` prefix for K8s resources; the actual K8s resource name is server-assigned) | MUST | DD-080 |
| REQ-XC-ID-020 | Conflict detection MUST be based on `id` (`dcm-instance-id` label). `metadata.name` is a non-unique `generateName` prefix and is NOT subject to uniqueness enforcement | MUST | DD-080, SC-001 |

#### Acceptance Criteria

##### AC-XC-ID-010: Dual identifier usage

- **Validates:** REQ-XC-ID-010
- **Given** a database is created with id "abc-123" and metadata.name "my-db"
- **When** the resource is stored
- **Then** `id` MUST be used in URL paths (`/databases/abc-123`) and as the `dcm.project/dcm-instance-id` label
- **And** `metadata.name` MUST be used as the `generateName` prefix for Kubernetes resources (e.g., `"my-db-"`)

##### AC-XC-ID-020: Conflict detection based on dcm-instance-id

- **Validates:** REQ-XC-ID-020
- **Given** a Cluster with label `dcm.project/dcm-instance-id=existing-id` already exists
- **When** a new database with a different `metadata.name` but the same id `"existing-id"` is created
- **Then** the request MUST be rejected with a conflict error

### 5.2 Resource Labeling

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LBL-010 | All Kubernetes resources managed by this SP (Cluster, Service, Secret) MUST carry the three DCM labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id={databaseId}`, `dcm.project/dcm-service-type=database` | MUST | DD-130 |
| REQ-XC-LBL-020 | If a create request specifies `metadata.labels` that collide with any DCM-reserved label key, the request MUST be rejected with 400 Bad Request | MUST | SC-004 |

**Label convention:**

| Label | Value | Description |
|-------|-------|-------------|
| dcm.project/managed-by | dcm | Identifies DCM-managed resources |
| dcm.project/dcm-instance-id | {databaseId} | Links K8s resource to database ID |
| dcm.project/dcm-service-type | database | Identifies the service type |

#### Acceptance Criteria

##### AC-XC-LBL-010: DCM labels applied to all resources

- **Validates:** REQ-XC-LBL-010
- **Given** any K8s resource (Cluster, Service, Secret) is created by the SP
- **When** the resource is applied to the cluster
- **Then** it MUST carry all three DCM labels with correct values

### 5.3 Error Handling

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ERR-010 | All HTTP error responses MUST conform to RFC 9457 (Problem Details for HTTP APIs) using the Error schema defined in the OpenAPI spec | MUST | |
| REQ-XC-ERR-020 | Error responses MUST set `Content-Type: application/problem+json` | MUST | |
| REQ-XC-ERR-030 | Error responses SHOULD include `detail` and `instance` fields. The `instance` field SHOULD be the request URI | SHOULD | |
| REQ-XC-ERR-040 | Error responses for INTERNAL errors MUST NOT expose implementation details such as stack traces, panic messages, raw dependency error strings, file paths, or memory addresses | MUST | |
| REQ-XC-ERR-050 | When handler-level validation detects multiple independent errors of the same type, the response MUST include an `errors` extension array where each entry contains a `detail` field describing one error | MUST | |
| REQ-XC-ERR-055 | In a multi-error validation response, the top-level `detail` field MUST contain a generic summary message (not a copy of any individual error) | MUST | |
| REQ-XC-ERR-060 | When only a single validation error is detected, the response MUST use the existing single-error format (top-level `detail` only) and MUST NOT include the `errors` array. The `errors` array MUST only be present when there are two or more validation errors | MUST | |
| REQ-XC-ERR-070 | When a handler-level validation error is associated with a specific request body field, the error SHOULD include a `pointer` field containing a JSON Pointer fragment identifier (RFC 6901 §6) rooted at the request body (e.g., `#/spec/resources/cpu/min`). For multi-error responses, each entry in the `errors` array carries its own `pointer`. For single-error responses, the top-level `Error` object carries the `pointer`. The `pointer` field MUST be absent when the error cannot be attributed to a single request body field. Label keys containing `/` or `~` MUST be escaped per RFC 6901 (`~` → `~0`, `/` → `~1`). This requirement applies only to handler-level validation; OpenAPI middleware validation errors are out of scope | SHOULD | See RFC 9457 §3 validation error example |

#### Acceptance Criteria

##### AC-XC-ERR-010: RFC 9457 compliance

- **Validates:** REQ-XC-ERR-010
- **Given** any error condition in the API
- **When** an error response is returned
- **Then** the body MUST conform to the RFC 9457 Error schema with at minimum `type` and `title` fields

##### AC-XC-ERR-020: Error content type

- **Validates:** REQ-XC-ERR-020
- **Given** any error response
- **When** the response is sent
- **Then** the `Content-Type` header MUST be `application/problem+json`

##### AC-XC-ERR-030: Instance field for tracing

- **Validates:** REQ-XC-ERR-030
- **Given** any error condition
- **When** the error response is returned
- **Then** the `instance` field SHOULD be set to the request URI

##### AC-XC-ERR-040: No implementation detail leakage

- **Validates:** REQ-XC-ERR-040
- **Given** an internal error occurs (unexpected store error, panic, or validation edge case)
- **When** the error response is returned
- **Then** the `detail` field MUST contain a generic message
- **And** the response MUST NOT contain stack traces, file paths, memory addresses, or raw internal error messages

##### AC-XC-ERR-050: Multi-error validation errors array

- **Validates:** REQ-XC-ERR-050
- **Given** a CreateDatabase request with two or more independent validation failures (e.g., CPU min>max AND a reserved DCM label)
- **When** the handler validates the request
- **Then** the response MUST be HTTP 400 with `type` = `INVALIDARGUMENT`
- **And** the body MUST contain an `errors` array with one entry per validation failure
- **And** the `errors` array MUST have at least 2 entries

##### AC-XC-ERR-055: Multi-error generic top-level detail

- **Validates:** REQ-XC-ERR-055
- **Given** a CreateDatabase request with two or more independent validation failures
- **When** the handler validates the request
- **Then** the top-level `detail` MUST be a generic summary message (not a copy of any individual error)

##### AC-XC-ERR-060: Single-error validation response

- **Validates:** REQ-XC-ERR-060
- **Given** a CreateDatabase request with exactly one validation failure
- **When** the handler validates the request
- **Then** the response MUST be HTTP 400 with `type` = `INVALIDARGUMENT`
- **And** the body MUST contain top-level `detail` with the error message
- **And** the body MUST NOT contain an `errors` array

##### AC-XC-ERR-070: Validation error field pointer

- **Validates:** REQ-XC-ERR-070
- **Given** a CreateDatabase request with one or more handler-level validation failures attributable to specific request body fields
- **When** the handler returns validation error(s)
- **Then** each error SHOULD include a `pointer` field containing a valid JSON Pointer fragment identifier (RFC 6901 §6) rooted at the request body (e.g., `#/spec/resources/cpu/min`)
- **And** for multi-error responses, each entry in the `errors` array MUST carry its own `pointer`
- **And** for single-error responses (per REQ-XC-ERR-060), the top-level `Error` object MUST carry the `pointer`
- **And** the `pointer` MUST be absent (null/omitted) when the error cannot be attributed to a single request body field
- **And** label keys containing `/` or `~` MUST be correctly escaped per RFC 6901 (`~` → `~0`, `/` → `~1`)
- **And** for cross-field constraints (e.g., cpu.min > cpu.max), the pointer MUST target the field whose value is asserted as exceeding the constraint

### 5.4 Logging

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LOG-010 | Structured logging MUST be used throughout the application | MUST | |
| REQ-XC-LOG-020 | Log level MUST be configurable via `SP_LOG_LEVEL` environment variable (default: `info`) | MUST | |

**Log level convention:**

| Level | Usage |
|-------|-------|
| ERROR | Unrecoverable failures, K8s API errors, NATS publish failures |
| WARN | Recoverable issues, retries |
| INFO | Lifecycle events, database create/delete operations |
| DEBUG | Detailed request/response data, informer events |

### 5.5 Configuration Management

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-CFG-010 | All configuration MUST be loadable from environment variables | MUST | |
| REQ-XC-CFG-020 | The SP MUST fail fast on startup when required configuration values are absent or empty, before starting any subsystem | MUST | |
| REQ-XC-CFG-030 | `SP_CNPG_EXTERNAL_SERVICE_TYPE` is required and MUST be one of `LoadBalancer` or `NodePort`; the SP MUST fail fast on startup if missing or invalid | MUST | |

#### Acceptance Criteria

##### AC-XC-CFG-010: Environment variable configuration

- **Validates:** REQ-XC-CFG-010
- **Given** any configuration value
- **When** the corresponding environment variable is set
- **Then** the SP MUST use the value from the environment variable

##### AC-XC-CFG-020: Fail-fast on missing required config

- **Validates:** REQ-XC-CFG-020
- **Given** a required config value (SP_NATS_URL, SP_NAME, SP_K8S_NAMESPACE, or SP_CNPG_EXTERNAL_SERVICE_TYPE) is absent or empty
- **When** the SP starts
- **Then** the SP MUST return an error identifying the missing field
- **And** MUST exit before starting the HTTP server or any subsystem

##### AC-XC-CFG-030: Fail-fast on missing or invalid ExternalServiceType

- **Validates:** REQ-XC-CFG-030
- **Given** SP_CNPG_EXTERNAL_SERVICE_TYPE is absent, empty, or set to an invalid value (e.g., `"ClusterIP"`, `"InvalidType"`)
- **When** the SP starts
- **Then** the SP MUST return an error identifying the invalid configuration
- **And** MUST exit before starting the HTTP server or any subsystem

---

## 6. Consolidated Configuration Reference

All configuration is loaded from environment variables.

| Config Key | Env Var | Default | Required | Topic |
|------------|---------|---------|----------|-------|
| server.address | SP_SERVER_ADDRESS | :8080 | No | 1 |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | No | 1 |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | No | 1 |
| kubernetes.namespace | SP_K8S_NAMESPACE | default | No | 5 |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (auto) | No | 5 |
| cnpg.externalServiceType | SP_CNPG_EXTERNAL_SERVICE_TYPE | - | Yes | 5 |
| cnpg.defaultVersion | SP_CNPG_DEFAULT_VERSION | 18 | No | 5 |
| cnpg.defaultStorageClass | SP_CNPG_DEFAULT_STORAGE_CLASS | (optional) | No | 5 |
| cnpg.imageCatalog | SP_CNPG_IMAGE_CATALOG | (optional) | No | 5 |
| nats.url | SP_NATS_URL | - | Yes | 6 |
| provider.name | SP_NAME | - | Yes | 6 |
| nats.subject | SP_NATS_SUBJECT | dcm.database | No | 6 |
| monitoring.debounceWindow | SP_MONITOR_DEBOUNCE_WINDOW | 2s | No | 6 |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | No | 6 |
| logging.level | SP_LOG_LEVEL | info | No | all |

---

## 7. Design Decisions

Design decisions are maintained in `.ai/decisions/cnpg-database-sp.decisions.md`.
Each entry captures the decision, rationale, trade-offs, and related requirements.

| ID | Title | Applies To |
|----|-------|------------|
| DD-010 | CNPG Clusters as the database abstraction unit | REQ-CNPG-010 |
| DD-020 | Introducing DELETING as an intermediate status | REQ-MON-090, REQ-MON-155 |
| DD-030 | Single configured namespace for all resources | REQ-CNPG-010, REQ-MON-010 |
| DD-040 | Repository pattern for store abstraction | REQ-STR-010, REQ-HLT-070 |
| DD-050 | Health check verifies CNPG operator presence | REQ-HLT-030, REQ-HLT-050 |
| DD-060 | Disabling CNPG ro and r services | REQ-CNPG-090, REQ-CNPG-100 |
| DD-070 | External visibility via a second service | REQ-CNPG-110 |
| DD-080 | generateName for Kubernetes resource naming | REQ-CNPG-020, REQ-XC-ID-010 |
| DD-090 | Memory unit conversion (MB→Mi, GB→Gi, TB→Ti) | REQ-CNPG-060 |
| DD-100 | Three SharedIndexInformers for layered status | REQ-MON-010 |
| DD-110 | DELETING-before-DELETED sequencing | REQ-MON-090, REQ-MON-155 |
| DD-120 | Instance ID and status in CloudEvent data payload | REQ-MON-145, REQ-MON-150 |
| DD-130 | DCM labels as the sole ownership mechanism | REQ-XC-LBL-010, REQ-MON-020 |
| DD-140 | Unlimited NATS reconnection | REQ-MON-180 |
| DD-150 | connection_details is output-only (AEP-203) | REQ-API-140, REQ-API-200 |
| DD-160 | provider_hints.postgres.initdb is input-only (AEP-203) | REQ-CNPG-140, REQ-CNPG-150 |

---

## 8. Spec Clarifications

Spec clarifications resolve ambiguities discovered during review or
implementation that are not explicitly covered by the primary requirements.

### SC-001: id uniqueness is by dcm-instance-id label, not by Kubernetes name

**Clarification:** Two database resources are considered duplicates only when
they share the same `dcm.project/dcm-instance-id` label value. The Kubernetes
resource name (generated via `generateName`) is not subject to uniqueness
enforcement. A create request with the same `id` as an existing Cluster MUST
be rejected with a conflict error even if the `metadata.name` differs.

**Applies to:** REQ-STR-030, REQ-CNPG-030, REQ-CNPG-190, REQ-XC-ID-020

---

### SC-002: min > max validation is server-side, not OpenAPI schema

**Clarification:** The `cpu.min > cpu.max` and `memory.min > memory.max`
cross-field validations cannot be expressed in OpenAPI 3.1 schema constraints
(which handle per-field patterns). These validations MUST be performed
explicitly in the API handler or store after parsing the request body. A
violation MUST return 400 with `type=INVALID_ARGUMENT`.

**Applies to:** REQ-API-130

---

### SC-003: rw service is always present

**Clarification:** The CNPG `rw` ClusterIP service is created by default and
cannot be disabled — it is the primary connection target. The SP disables `ro`
and `r` services via CNPG cluster configuration. There is no way to suppress
the `rw` service; it will always appear in the `services` array.

**Applies to:** REQ-CNPG-090

---

### SC-004: DCM label collision returns 400, not 409

**Clarification:** If a create request supplies `metadata.labels` that overlap
with any DCM-reserved label key (`dcm.project/managed-by`,
`dcm.project/dcm-instance-id`, `dcm.project/dcm-service-type`), the request
MUST be rejected with 400 Bad Request (INVALID_ARGUMENT), not 409 Conflict.
The collision is a request validation error, not a resource-existence conflict.

**Applies to:** REQ-XC-LBL-020, REQ-API-250

---

### SC-005: provider_hints.postgres is passed through without transformation

**Clarification:** The SP maps `provider_hints.postgres.initdb.*` values
directly to the corresponding CNPG `Cluster.spec.bootstrap.initdb.*` fields.
No transformation or enrichment is applied. If a key is not recognized by
CNPG, CNPG will ignore it. The SP is not responsible for validating the
semantic correctness of `provider_hints` values beyond what is required to
construct a valid Cluster resource.

**Applies to:** REQ-CNPG-140, REQ-CNPG-160

---

### SC-006: Invalid page_token returns 400

**Clarification:** The `page_token` parameter is an opaque server-generated
token. If a client supplies a value that cannot be decoded or that references
a page position that no longer exists, the SP MUST return 400 Bad Request
with `type=INVALID_ARGUMENT`. The SP MUST NOT silently fall back to returning
the first page.

**Applies to:** REQ-API-160

---

### SC-007: connection_details states

**Clarification:** `connection_details` has exactly two observable states:

- **Empty string (`""`)**: The database is not yet RUNNING (status is PENDING,
  FAILED, UNKNOWN, DELETING, or DELETED). Credentials are not available.
- **Non-empty base64-encoded string**: The database is RUNNING. The string
  encodes connection details including host, port, database name, username,
  and password derived from the CNPG-managed Secret.

The field is never `null` or absent from responses.

**Applies to:** REQ-API-140, REQ-API-200

---

### SC-008: Delete with no Secrets succeeds

**Clarification:** `REQ-CNPG-180` requires explicitly deleting Secrets with
the matching `dcm-instance-id` label. If no such Secrets exist (because the
database was created without an `initdb.password`), the delete operation MUST
still succeed. The Secret deletion step MUST treat "no secrets found" as a
no-op, not as an error.

**Applies to:** REQ-CNPG-180

---

### SC-009: Pod role label filtering (cnpg.io/podRole=instance)

**Clarification:** CNPG creates multiple Pod types in a cluster namespace:
instance Pods (the primary/replica PostgreSQL servers), backup Pods, and
potentially pgBouncer Pods. Only Pods with `cnpg.io/podRole=instance` represent
database instances. The Pod informer's label selector MUST include this filter.
Reconciling status from backup or pgBouncer Pods would produce incorrect
PENDING/RUNNING/FAILED signals.

**Applies to:** REQ-MON-040

---

## 9. Requirement ID Index

| Prefix | Topic | Count |
|--------|-------|-------|
| REQ-HTTP-NNN | 4.1: HTTP Server | 11 |
| REQ-HLT-NNN | 4.2: Health Service | 8 |
| REQ-API-NNN | 4.3: Database API Handlers | 25 |
| REQ-STR-NNN | 4.4: Database Store Interface | 8 |
| REQ-CNPG-NNN | 4.5: CNPG Integration | 20 |
| REQ-MON-NNN | 4.6: Status Monitoring | 23 |
| REQ-REG-NNN | 4.7: DCM Registration | 2 |
| REQ-XC-ID-NNN | 5.1: Resource Identity | 2 |
| REQ-XC-LBL-NNN | 5.2: Resource Labeling | 2 |
| REQ-XC-ERR-NNN | 5.3: Error Handling | 8 |
| REQ-XC-LOG-NNN | 5.4: Logging | 2 |
| REQ-XC-CFG-NNN | 5.5: Configuration Management | 3 |
| DD-NNN | 7: Design Decisions | 16 |
| SC-NNN | 8: Spec Clarifications | 9 |
| **Total requirements** | | **114** |
