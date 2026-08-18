# Test Plan: CNPG Database SP — Integration Tests

## Overview

- **Related Spec:** .ai/specs/cnpg-database-sp.spec.md
- **Related Requirements:** REQ-HTTP-010–110, REQ-HLT-010–080, REQ-API-010–250, REQ-STR-010–080, REQ-CNPG-010–200, REQ-MON-010–185, REQ-REG-010–020, REQ-XC-ERR-010–070, REQ-XC-LBL-010–020, REQ-XC-LOG-010–020, REQ-XC-CFG-010–030
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-08-19
- **Last Updated:** 2026-08-19

Integration tests verify the service as a whole, combining multiple components
wired together. External dependencies are replaced with lightweight in-process
fakes:

| Component | Replacement |
|-----------|-------------|
| Kubernetes API server | `k8s.io/client-go/kubernetes/fake` |
| CNPG Cluster CRD | `k8s.io/client-go/dynamic/fake` |
| HTTP server | `net/http/httptest.Server` |
| NATS | Embedded NATS server (`nats-server/v2/test`) |

Tests assert on:
- HTTP status codes and response bodies from `httptest.Server`
- Kubernetes objects created or mutated in the fake client
- NATS messages received on `dcm.database`

### Utility Transitive Coverage

Helper functions (resource conversion, label builders, indexer functions,
debounce, CloudEvent construction) are **not** verified in isolated unit blocks.
Instead, each integration test case references the utility TC-ID(s) it exercises
under a `- **Transitively covers:**` line.

---

## 1 · HTTP Server Lifecycle

> **Suggested Ginkgo structure:** `Describe("HTTP Server Lifecycle")`

### TC-I001: Server starts and listens on configured address

- **Requirement:** REQ-HTTP-010
- **Priority:** High
- **Type:** Integration
- **Given:** Server is started with `SP_SERVER_ADDRESS=":0"` (random available port)
- **When:** A `GET /api/v1alpha1/databases/health` request is sent to the assigned address
- **Then:** HTTP status is `200`

### TC-I002: Server shuts down gracefully when context is cancelled

- **Requirement:** REQ-HTTP-040
- **Priority:** High
- **Type:** Integration
- **Given:** A running server with an in-flight long request (mocked store delays)
- **When:** The context is cancelled
- **Then:** In-flight request completes before the server exits AND new connections are refused

### TC-I003: Request timeout is enforced

- **Requirement:** REQ-HTTP-110
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_SERVER_REQUEST_TIMEOUT="100ms"` AND a handler that blocks for 500ms (mocked store)
- **When:** A request is sent
- **Then:** Response is HTTP `503` within ~150ms

### TC-I004: Incoming requests are logged

- **Requirement:** REQ-XC-LOG-010
- **Priority:** Medium
- **Type:** Integration
- **Given:** A running server with a structured logger captured in a buffer
- **When:** Any HTTP request completes
- **Then:** The log buffer contains a structured entry with at minimum `method`, `path`, and `status` fields

### TC-I005: Response logs include duration field

- **Requirement:** REQ-HTTP-060, REQ-XC-LOG-020
- **Priority:** Medium
- **Type:** Integration
- **Given:** A running server with a logger captured in a buffer
- **When:** A request completes
- **Then:** The log entry contains a `duration_ms` (or equivalent) numeric field `>= 0`

### TC-I006: Middleware sets Content-Type: application/json on success

- **Requirement:** REQ-API-010
- **Priority:** High
- **Type:** Integration
- **Given:** A running server
- **When:** `GET /api/v1alpha1/databases` returns `200`
- **Then:** `Content-Type` response header contains `application/json`

### TC-I007: Unknown routes return 404

- **Requirement:** REQ-HTTP-010
- **Priority:** Medium
- **Type:** Integration
- **Given:** A running server
- **When:** `GET /api/v1alpha1/unknown` is requested
- **Then:** HTTP status is `404`

### TC-I008: max_page_size boundary enforcement via OpenAPI middleware

- **Requirement:** REQ-HTTP-090
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U057
- **Given:** OpenAPI validation middleware is active (embedded spec)
- **When:** `GET /api/v1alpha1/databases?max_page_size=0` is requested
- **Then:** HTTP status is `400` (rejected by middleware before handler is called)

---

## 2 · Database API — Create

> **Suggested Ginkgo structure:** `Describe("POST /api/v1alpha1/databases")`

### TC-I009: Create a database end-to-end returns 201

- **Requirement:** REQ-API-020, REQ-STR-010
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U008, TC-U024 (interface contracts satisfied in wired-up code)
- **Given:** A wired-up server with a fake K8s client and no pre-existing Clusters
- **When:** `POST /api/v1alpha1/databases` is called with a valid body
- **Then:**
  - HTTP status is `201`
  - Response body contains `id`, `path`, `status="PENDING"`, `create_time`, `update_time`, `connection_details=""`
  - One CNPG Cluster resource exists in the fake K8s client

### TC-I010: Created Cluster has all three DCM labels

- **Requirement:** REQ-XC-LBL-010
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U094
- **Given:** A wired-up server with a fake K8s client
- **When:** `POST /api/v1alpha1/databases?id=abc-123` is called
- **Then:** The Cluster in the fake K8s client has:
  - `labels["dcm.project/managed-by"] = "dcm"`
  - `labels["dcm.project/dcm-instance-id"] = "abc-123"`
  - `labels["dcm.project/dcm-service-type"] = "database"`

### TC-I011: Created Cluster uses generateName based on metadata.name

- **Requirement:** REQ-CNPG-020
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `metadata.name="my-db"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's `metadata.generateName` is `"my-db-"` AND `metadata.name` is empty

### TC-I012: Resource requests and limits set from spec.resources.cpu

- **Requirement:** REQ-CNPG-050
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U027
- **Given:** POST body with `spec.resources.cpu.min="500m"` and `spec.resources.cpu.max="2"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's instance template has `resources.requests.cpu="500m"` and `resources.limits.cpu="2"`

### TC-I013: Memory values converted from schema format to Kubernetes format

- **Requirement:** REQ-CNPG-060
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U028
- **Given:** POST body with `spec.resources.memory.min="1GB"` and `spec.resources.memory.max="4GB"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster has `resources.requests.memory="1Gi"` and `resources.limits.memory="4Gi"`

### TC-I014: spec.replicas sets CNPG Cluster instances count

- **Requirement:** REQ-CNPG-040
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.replicas=3`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's `spec.instances` is `3`

### TC-I015: Default replicas = 1 when not specified

- **Requirement:** REQ-CNPG-040
- **Priority:** High
- **Type:** Integration
- **Given:** POST body without `spec.replicas`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's `spec.instances` is `1`

### TC-I016: internal visibility creates ClusterIP service only

- **Requirement:** REQ-CNPG-090, REQ-CNPG-100
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.network.visibility="internal"` (or omitted — default)
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:**
  - No external (LoadBalancer/NodePort) Service is created by the SP
  - CNPG `ro` and `r` services are disabled in the Cluster spec

### TC-I017: external visibility creates external Service

- **Requirement:** REQ-CNPG-110
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE="LoadBalancer"` AND POST body with `spec.network.visibility="external"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:**
  - An additional K8s Service of type `LoadBalancer` exists for the database
  - The Service has the three DCM labels

### TC-I018: external visibility uses NodePort when configured

- **Requirement:** REQ-CNPG-110
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE="NodePort"` AND POST body with `spec.network.visibility="external"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The external Service type is `NodePort`

### TC-I019: CNPG ro and r services are disabled on created Cluster

- **Requirement:** REQ-CNPG-090
- **Priority:** High
- **Type:** Integration
- **Given:** A POST with any visibility
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster spec disables `enableReadOnlyService` and `enableReadService` (or equivalent CNPG API fields)

### TC-I020: provider_hints.postgres.storage.storage_class applied to Cluster

- **Requirement:** REQ-CNPG-130
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.provider_hints.postgres.storage.storage_class="fast-storage"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's storage `storageClass` is `"fast-storage"`

### TC-I021: Default storage class applied from config when provider_hints not specified

- **Requirement:** REQ-CNPG-130
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_CNPG_DEFAULT_STORAGE_CLASS="default-sc"` AND POST body without `provider_hints.postgres.storage`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's storage `storageClass` is `"default-sc"`

### TC-I022: provider_hints.initdb.database sets bootstrap database name

- **Requirement:** REQ-CNPG-140
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.provider_hints.postgres.initdb.database="appdb"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's `spec.bootstrap.initdb.database` is `"appdb"`

### TC-I023: provider_hints.initdb.user sets bootstrap user

- **Requirement:** REQ-CNPG-140
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.provider_hints.postgres.initdb.user="appuser"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's `spec.bootstrap.initdb.owner` is `"appuser"`

### TC-I024: provider_hints.initdb.password creates kubernetes.io/basic-auth Secret

- **Requirement:** REQ-CNPG-150
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.provider_hints.postgres.initdb.password="s3cr3t"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:**
  - A `kubernetes.io/basic-auth` Secret exists in the namespace
  - The Secret has `data.password` matching `base64("s3cr3t")`
  - The Cluster references this Secret in its initdb config

### TC-I025: provider_hints.initdb fields are input-only (not returned)

- **Requirement:** REQ-CNPG-160
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.provider_hints.postgres.initdb.password="s3cr3t"`
- **When:** `POST /api/v1alpha1/databases` returns `201` AND a subsequent `GET /api/v1alpha1/databases/{id}` is called
- **Then:** The GET response does NOT include `spec.provider_hints.postgres.initdb.password`

### TC-I026: Create with client-specified id is idempotent on retry

- **Requirement:** REQ-API-040
- **Priority:** High
- **Type:** Integration
- **Given:** `POST /api/v1alpha1/databases?id=abc-123` succeeds once
- **When:** The same `POST` is sent again with the same `id`
- **Then:** HTTP status is `409` AND body is RFC 9457 `ALREADY_EXISTS`

### TC-I027: spec.version sets PostgreSQL major version

- **Requirement:** REQ-CNPG-080
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.version="16"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster's image or `spec.imageName` targets PostgreSQL 16

### TC-I028: Default version applied from config when not specified

- **Requirement:** REQ-CNPG-080
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_CNPG_DEFAULT_VERSION="18"` AND POST body without `spec.version`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The Cluster targets PostgreSQL 18

### TC-I069: Create returns 409 when DCM instance-id label collision detected

- **Requirement:** REQ-XC-ID-020
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U026 (conflict error is distinguishable)
- **Given:** A Cluster already exists with label `dcm.project/dcm-instance-id="abc-123"`
- **When:** `POST /api/v1alpha1/databases?id=abc-123` is called
- **Then:** HTTP status is `409` AND RFC 9457 body contains type `ALREADY_EXISTS`

### TC-I086: spec.resources.storage maps to Cluster storage size

- **Requirement:** REQ-CNPG-070
- **Priority:** High
- **Type:** Integration
- **Given:** POST body with `spec.resources.storage="50GB"`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The created Cluster's `spec.storage.size` is `"50Gi"`

### TC-I087: image_catalog server config is set on Cluster

- **Requirement:** REQ-CNPG-200
- **Priority:** Medium
- **Type:** Integration
- **Given:** SP is started with `SP_CNPG_IMAGE_CATALOG=my-catalog`
- **When:** `POST /api/v1alpha1/databases` is called
- **Then:** The created Cluster has `spec.imageCatalogRef.name` set to `"my-catalog"`

### TC-I083: Custom port is propagated to CNPG Cluster configuration

- **Requirement:** REQ-CNPG-120
- **Priority:** High
- **Type:** Integration
- **Given:** A fake Kubernetes client
- **When:** `POST /api/v1alpha1/databases` is called with `network.port=5433` in the request body
- **Then:** A CNPG `Cluster` resource is created in the fake client
- **And:** The Cluster's port configuration contains port `5433`

---

## 3 · Database API — Read & List

> **Suggested Ginkgo structure:** `Describe("GET /api/v1alpha1/databases")`
> and `Describe("GET /api/v1alpha1/databases/{id}")`

### TC-I029: ListDatabases returns only SP-managed databases

- **Requirement:** REQ-API-150, REQ-XC-LBL-010
- **Priority:** High
- **Type:** Integration
- **Given:** 3 SP-managed Clusters (with DCM labels) and 1 unmanaged Cluster exist in the fake K8s client
- **When:** `GET /api/v1alpha1/databases` is called
- **Then:** Response contains exactly 3 results

### TC-I030: ListDatabases results include all response fields

- **Requirement:** REQ-API-150
- **Priority:** High
- **Type:** Integration
- **Given:** One SP-managed Cluster exists with replicas=2
- **When:** `GET /api/v1alpha1/databases` is called
- **Then:** The result item contains `id`, `path`, `status`, `spec`, `metadata`, `create_time`, `update_time`, `connection_details`

### TC-I031: ListDatabases pagination — page_token iterates all results

- **Requirement:** REQ-API-160
- **Priority:** High
- **Type:** Integration
- **Given:** 25 SP-managed Clusters exist and `max_page_size=10`
- **When:** Pages are fetched using `page_token` from each response until `next_page_token` is absent
- **Then:** Total unique results across all pages equals `25`

### TC-I084: ListDatabases defaults to max_page_size=50 when not specified

- **Requirement:** REQ-STR-060
- **Priority:** High
- **Type:** Integration
- **Given:** 60 SP-managed Clusters exist in the fake Kubernetes client
- **When:** `GET /api/v1alpha1/databases` is called with no `max_page_size` query parameter
- **Then:** HTTP status is `200`
- **And:** The response `items` array contains exactly 50 entries
- **And:** `next_page_token` is present (indicating there are more results)

### TC-I032: GetDatabase returns 404 for non-existent id

- **Requirement:** REQ-API-190
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U025 (not-found error is distinguishable)
- **Given:** No Cluster exists with `dcm.project/dcm-instance-id="xyz-999"`
- **When:** `GET /api/v1alpha1/databases/xyz-999` is called
- **Then:** HTTP status is `404` AND RFC 9457 body contains type `NOT_FOUND`

### TC-I033: GetDatabase returns connection_details when RUNNING

- **Requirement:** REQ-API-200
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster exists with status `readyInstances=1` AND its associated Pod is Running AND the initdb Secret exists with known password
- **When:** `GET /api/v1alpha1/databases/{id}` is called after the informer fires RUNNING
- **Then:** `connection_details` is a non-empty base64-encoded string
- **And:** Decoded JSON contains `username`, `password`, `hostname`, `port`, `database`, `pgpass`, `uri`, `jdbc-uri`

### TC-I034: GetDatabase returns empty connection_details when not RUNNING

- **Requirement:** REQ-API-200
- **Priority:** High
- **Type:** Integration (table-driven)
- **Given:** A database in each non-RUNNING status (PENDING, FAILED, UNKNOWN, DELETING)
- **When:** `GET /api/v1alpha1/databases/{id}` is called for each
- **Then:** `connection_details` is an empty string

### TC-I035: GetDatabase includes services[] array

- **Requirement:** REQ-CNPG-090, SC-003
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster with internal visibility exists
- **When:** `GET /api/v1alpha1/databases/{id}` is called
- **Then:** Response includes a `services` array with at least one entry (the `rw` ClusterIP Service)

### TC-I036: GetDatabase services[] includes external service when visibility=external

- **Requirement:** REQ-CNPG-110
- **Priority:** High
- **Type:** Integration
- **Given:** `SP_CNPG_EXTERNAL_SERVICE_TYPE="LoadBalancer"` AND a Cluster with `visibility=external`
- **When:** `GET /api/v1alpha1/databases/{id}` is called
- **Then:** `services` contains an entry with `type="LoadBalancer"` and an assigned address

---

## 4 · Database API — Delete

> **Suggested Ginkgo structure:** `Describe("DELETE /api/v1alpha1/databases/{id}")`

### TC-I037: DeleteDatabase returns 204 and removes Cluster

- **Requirement:** REQ-API-210, REQ-CNPG-170
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster with `dcm.project/dcm-instance-id="abc-123"` exists
- **When:** `DELETE /api/v1alpha1/databases/abc-123` is called
- **Then:**
  - HTTP status is `204`
  - The Cluster has a `deletionTimestamp` set in the fake K8s client (not yet deleted — K8s GC pending)

### TC-I038: DeleteDatabase cascades to associated Secret

- **Requirement:** REQ-CNPG-180
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster and associated `kubernetes.io/basic-auth` Secret exist
- **When:** `DELETE /api/v1alpha1/databases/{id}` is called
- **Then:** The Secret is also marked for deletion (or deleted synchronously)

### TC-I039: DeleteDatabase returns 404 for non-existent database

- **Requirement:** REQ-API-220
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U025
- **Given:** No Cluster exists with `dcm.project/dcm-instance-id="xyz-999"`
- **When:** `DELETE /api/v1alpha1/databases/xyz-999` is called
- **Then:** HTTP status is `404` AND RFC 9457 body contains type `NOT_FOUND`

### TC-I040: DeleteDatabase cascades to external Service

- **Requirement:** REQ-CNPG-170
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster with an associated external LoadBalancer Service exists
- **When:** `DELETE /api/v1alpha1/databases/{id}` is called
- **Then:** The external Service is also deleted (or has owner reference to Cluster for GC)

---

## 5 · CNPG Store

> **Suggested Ginkgo structure:** `Describe("CNPG Store")`

### TC-I041: Store.CheckHealth returns nil when K8s API reachable and CNPG operator running

- **Requirement:** REQ-HLT-070, REQ-STR-010
- **Priority:** High
- **Type:** Integration
- **Given:** Fake K8s client returns successfully AND a CNPG operator Pod exists in the expected namespace
- **When:** `store.CheckHealth(ctx)` is called
- **Then:** Returns `nil` error

### TC-I042: Objects without dcm.project/dcm-instance-id label are skipped by informer

- **Requirement:** REQ-MON-030
- **Priority:** Medium
- **Type:** Integration
- **Transitively covers:** TC-U041
- **Given:** A Cluster without the `dcm.project/dcm-instance-id` label is added to the fake K8s client
- **When:** The informer fires for that object
- **Then:** No status event is published to NATS

### TC-I043: Indexer function retrieves objects by dcm.project/dcm-instance-id

- **Requirement:** REQ-MON-030
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U040, TC-U042
- **Given:** Three Clusters exist — two with `dcm.project/dcm-instance-id="abc-123"` and one with `"xyz-456"`
- **When:** The informer indexer is queried for `"abc-123"`
- **Then:** Exactly 2 objects are returned

### TC-I044: Lookup by DCM instance ID returns correct resource

- **Requirement:** REQ-CNPG-030, REQ-STR-040
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U040
- **Given:** A Cluster exists with `dcm.project/dcm-instance-id="abc-123"` and `metadata.name="my-db-abc"` in the fake K8s client
- **When:** The store looks up by id `"abc-123"`
- **Then:** The returned resource has `metadata.name="my-db-abc"`

---

## 6 · Status Monitoring

> **Suggested Ginkgo structure:** `Describe("Status Monitoring")`

### TC-I045: Informers start and list existing resources

- **Requirement:** REQ-MON-010, REQ-MON-020
- **Priority:** High
- **Type:** Integration
- **Given:** A fake K8s client is pre-populated with one Cluster, one Pod (with `cnpg.io/podRole=instance`), and one PVC
- **When:** All three informers are started
- **Then:** After the sync timeout, each informer's local cache contains the pre-existing resource

### TC-I046: Status event published when Cluster informer fires

- **Requirement:** REQ-MON-050
- **Priority:** High
- **Type:** Integration
- **Given:** An embedded NATS server and a Cluster informer watching a fake K8s client
- **When:** A Cluster is created in the fake K8s client with `dcm.project/dcm-instance-id="abc-123"`
- **Then:** A NATS message is received on subject `dcm.database` within the debounce window + buffer

### TC-I047: Published CloudEvent has correct v1.0 structure

- **Requirement:** REQ-MON-145, REQ-MON-150
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U036
- **Given:** An embedded NATS server and a Cluster whose status drives a RUNNING event
- **When:** The status event is published
- **Then:** The NATS message payload is a valid CloudEvent with:
  - `specversion = "1.0"`
  - `type = "dcm.status.database"`
  - `source = "dcm/providers/{SP_NAME}"`
  - `subject = "dcm.database"` (configurable via SP_NATS_SUBJECT)
  - `data.id` matching the database id
  - `data.status` matching the expected DCM status

### TC-I048: DELETING CloudEvent published on delete

- **Requirement:** REQ-MON-155
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U037
- **Given:** An embedded NATS server
- **When:** `DELETE /api/v1alpha1/databases/{id}` is called (Cluster gets deletionTimestamp)
- **Then:** A NATS message is received on `dcm.database` with `data.status = "DELETING"`

### TC-I049: FAILED CloudEvent includes reason in message

- **Requirement:** REQ-MON-150
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U095
- **Given:** A Pod with `status.containerStatuses[0].state.waiting.reason="CrashLoopBackOff"` and `dcm.project/dcm-instance-id="abc-123"`
- **When:** The Pod informer fires
- **Then:** The NATS `data.message` contains `"CrashLoopBackOff"`

### TC-I050: DELETED CloudEvent published when Cluster is fully removed

- **Requirement:** REQ-MON-100
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster exists, a DELETING event was published, and the Cluster is subsequently removed from the fake K8s client
- **When:** The Cluster informer fires the deletion event
- **Then:** A NATS message is received with `data.status = "DELETED"`

### TC-I062: Pod Running → RUNNING event published

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Running mapping)
- **Given:** A Cluster exists with `readyInstances=0` AND no Pod exists
- **When:** A Pod with `cnpg.io/podRole=instance` and `phase=Running` is added to the fake K8s client
- **Then:** Status resolves to `RUNNING` (Pod takes precedence over Cluster) AND NATS receives `data.status="RUNNING"`

### TC-I063: Pod Failed → FAILED event published

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Failed mapping)
- **Given:** A Pod with `cnpg.io/podRole=instance` and `phase=Failed`
- **When:** The Pod informer fires
- **Then:** NATS receives `data.status="FAILED"`

### TC-I064: Pod Unknown → UNKNOWN event published

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Unknown mapping)
- **Given:** A Pod with `cnpg.io/podRole=instance` and `phase=Unknown`
- **When:** The Pod informer fires
- **Then:** NATS receives `data.status="UNKNOWN"`

### TC-I065: PVC Lost + Pod Pending → FAILED event

- **Requirement:** REQ-MON-070
- **Priority:** High
- **Type:** Integration
- **Given:** A Pod with `phase=Pending` AND its associated PVC with `phase=Lost`
- **When:** The PVC informer fires
- **Then:** NATS receives `data.status="FAILED"`

### TC-I066: Rapid status changes debounced — only last published

- **Requirement:** REQ-MON-160
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U038
- **Given:** `SP_MONITOR_DEBOUNCE_WINDOW="200ms"` (short for test speed) AND an embedded NATS server
- **When:** Three status changes (PENDING, RUNNING, FAILED) are injected within 100ms for instance `"abc-123"`
- **Then:** Exactly one NATS message is received AND its `data.status` is `"FAILED"`

### TC-I067: Status changes separated by debounce window published separately

- **Requirement:** REQ-MON-160
- **Priority:** Medium
- **Type:** Integration
- **Transitively covers:** TC-U039
- **Given:** `SP_MONITOR_DEBOUNCE_WINDOW="100ms"` AND an embedded NATS server
- **When:** A RUNNING event is injected, then 150ms elapses, then a FAILED event is injected
- **Then:** Two NATS messages are received for the instance — one `RUNNING` and one `FAILED`

### TC-I068: Debounce is per-instance (one instance does not suppress another)

- **Requirement:** REQ-MON-165
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U096
- **Given:** `SP_MONITOR_DEBOUNCE_WINDOW="200ms"` AND an embedded NATS server
- **When:** Rapid changes occur for instance `"abc-123"` while instance `"xyz-456"` changes once
- **Then:** A NATS message for `"xyz-456"` arrives without waiting for `"abc-123"`'s debounce to expire

### TC-I073: Cluster informer resync period causes periodic re-evaluation

- **Requirement:** REQ-MON-010
- **Priority:** Medium
- **Type:** Integration
- **Given:** `SP_MONITOR_RESYNC_PERIOD="200ms"` (short for test speed) and a stable Cluster
- **When:** The resync timer fires at least once
- **Then:** A NATS message is published for the stable database (confirming periodic re-evaluation)

### TC-I074: Pod informer only tracks pods with cnpg.io/podRole=instance

- **Requirement:** REQ-MON-040
- **Priority:** High
- **Type:** Integration
- **Given:** Two pods exist: one with `cnpg.io/podRole=instance` and one with `cnpg.io/podRole=pooler`
- **When:** Both pods update to `phase=Failed`
- **Then:** Only one NATS message is published (for the `instance` pod, not the `pooler`)

### TC-I075: Multi-replica: all RUNNING → RUNNING aggregate

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster with `spec.instances=3` AND 3 Pods with `phase=Running`
- **When:** All three Pod informers fire
- **Then:** NATS receives `data.status="RUNNING"`

### TC-I076: Multi-replica: one FAILED → FAILED aggregate

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Integration
- **Given:** A Cluster with `spec.instances=3` AND 2 Pods with `phase=Running`, 1 Pod with `phase=Failed`
- **When:** All three Pod informers have fired
- **Then:** NATS receives `data.status="FAILED"`

### TC-I112: NATS disconnect — SP continues serving HTTP and resumes publishing on reconnect

- **Requirement:** REQ-MON-180
- **Priority:** High
- **Type:** Integration (placeholder — requires embedded NATS with simulated disconnect)
- **Given:** A running SP publishing status events to an embedded NATS server
- **When:** The NATS server is stopped (simulating a network partition)
- **Then:** The SP continues to serve HTTP requests and returns correct status codes
- **And:** After the NATS server is restarted, the next status change is successfully published to NATS
- **And:** The NATS disconnect and reconnect events are logged at ERROR and INFO level respectively
- **TODO:** Implement using a controllable embedded NATS server that can be stopped/restarted mid-test. The current test harness uses a simple embedded NATS; extend `BeforeSuite` setup to expose a stop/restart handle.

### TC-I082: Initial status sync publishes CloudEvent for every existing Cluster on startup

- **Requirement:** REQ-MON-185
- **Priority:** High
- **Type:** Integration
- **Given:** A fake Kubernetes client pre-populated with 2 managed CNPG Clusters before the SP starts
- **When:** The informer cache sync completes
- **Then:** NATS receives exactly 2 CloudEvents, one for each Cluster's DCM instance ID
- **And:** Each event reflects the Cluster's current reconciled status
- **And:** The events are published before any subsequent informer-driven events

---

## 7 · Health Check

> **Suggested Ginkgo structure:** `Describe("GET /api/v1alpha1/databases/health")`

### TC-I077: Health returns healthy when K8s API and CNPG operator are available

- **Requirement:** REQ-HLT-010, REQ-HLT-020, REQ-HLT-040, REQ-HLT-060
- **Priority:** High
- **Type:** Integration
- **Given:** Fake K8s client responds normally AND a CNPG operator Pod exists
- **When:** `GET /api/v1alpha1/databases/health` is called
- **Then:**
  - HTTP status is `200`
  - `Content-Type` response header contains `application/json`
  - Body `status` is `"healthy"`
  - `type` is `"cnpg-database-service-provider.dcm.io/health"`
  - `path` is `"health"`

### TC-I078: Health returns unhealthy when K8s API unreachable

- **Requirement:** REQ-HLT-060
- **Priority:** High
- **Type:** Integration
- **Given:** The store's K8s client returns an error on any API call
- **When:** `GET /api/v1alpha1/databases/health` is called
- **Then:**
  - HTTP status is `200`
  - Body `status` is `"unhealthy"`
  - `uptime` is an integer `>= 0`

### TC-I079: Health returns unhealthy when CNPG operator is not found

- **Requirement:** REQ-HLT-060
- **Priority:** High
- **Type:** Integration
- **Given:** Fake K8s client responds normally BUT no CNPG operator Pod or Deployment exists
- **When:** `GET /api/v1alpha1/databases/health` is called
- **Then:**
  - HTTP status is `200`
  - Body `status` is `"unhealthy"`

### TC-I080: Health uptime increases over time

- **Requirement:** REQ-HLT-020
- **Priority:** Medium
- **Type:** Integration
- **Given:** A wired-up server started at time T0
- **When:** `GET /api/v1alpha1/databases/health` is called at T0+2s
- **Then:** `uptime >= 2`

### TC-I081: Create rejects reserved "health" database ID

- **Requirement:** REQ-HLT-080
- **Priority:** High
- **Type:** Integration
- **Given:** A running server with a fake Kubernetes client
- **When:** `POST /api/v1alpha1/databases?id=health` is called with a valid request body
- **Then:** HTTP status is `400`
- **And:** The RFC 9457 body has `type` `INVALID_ARGUMENT`
- **And:** The `detail` field mentions the reserved ID
- **And:** No CNPG `Cluster` resource is created in the fake Kubernetes client

---

## 8 · Error Handling & Edge Cases

> **Suggested Ginkgo structure:** `Describe("Error Handling")`

### TC-I100: RFC 9457 error on malformed JSON body

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Integration
- **Given:** A running server
- **When:** `POST /api/v1alpha1/databases` is called with body `{not valid json}`
- **Then:** HTTP status is `400` AND `Content-Type` is `application/problem+json`

### TC-I101: RFC 9457 error on missing required body fields

- **Requirement:** REQ-XC-ERR-010
- **Priority:** High
- **Type:** Integration
- **Given:** A running server
- **When:** `POST /api/v1alpha1/databases` is called with body `{}`
- **Then:** HTTP status is `400` AND body contains `type` and `title` fields

### TC-I102: Error response includes instance field set to request path

- **Requirement:** REQ-XC-ERR-030
- **Priority:** High
- **Type:** Integration
- **Given:** Any request that causes an error response
- **When:** The response is received
- **Then:** The `instance` field equals the request path (e.g., `"/api/v1alpha1/databases/xyz-999"`)

### TC-I103: 5xx errors do not include internal stack traces in response body

- **Requirement:** REQ-XC-ERR-040
- **Priority:** High
- **Type:** Integration
- **Given:** A store that returns an unexpected internal error
- **When:** A request triggers the error
- **Then:** HTTP status is `500` AND the response body does NOT contain any Go stack trace or raw error string

### TC-I104: Internal error logged server-side with full detail

- **Requirement:** REQ-XC-ERR-040
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U051 (INTERNAL error type)
- **Given:** A logger captured in a buffer AND a store that returns an internal error
- **When:** A request triggers the error
- **Then:** The log buffer contains the full error detail AND the HTTP response body contains only a safe generic message

### TC-I105: Multi-error validation response includes errors array with generic top-level detail

- **Requirement:** REQ-XC-ERR-050, REQ-XC-ERR-055
- **Priority:** High
- **Type:** Integration
- **Given:** A running server
- **When:** `POST /api/v1alpha1/databases` is called with a body containing two or more independent handler-level validation failures (e.g., `spec.resources.cpu.min > spec.resources.cpu.max` AND `metadata.labels` containing a reserved DCM key such as `dcm.project/managed-by`)
- **Then:** HTTP status is `400` AND `Content-Type` is `application/problem+json`
- **And:** The body contains an `errors` array with at least 2 entries, each carrying a `detail` field describing one failure
- **And:** The top-level `detail` is a generic summary message and does NOT reproduce any individual entry from the `errors` array

### TC-I106: Single validation error response omits errors array and includes pointer

- **Requirement:** REQ-XC-ERR-060, REQ-XC-ERR-070
- **Priority:** High
- **Type:** Integration
- **Given:** A running server
- **When:** `POST /api/v1alpha1/databases` is called with exactly one handler-level validation failure (e.g., `spec.resources.cpu.min > spec.resources.cpu.max`)
- **Then:** HTTP status is `400` AND `Content-Type` is `application/problem+json`
- **And:** The body contains a top-level `detail` with the specific error message
- **And:** The body does NOT contain an `errors` array
- **And:** The top-level `Error` object includes a `pointer` field (e.g., `#/spec/resources/cpu/min`) identifying the offending field

### TC-I107: Panic in handler returns RFC 9457 500 INTERNAL response

- **Requirement:** REQ-HTTP-070
- **Priority:** High
- **Type:** Integration
- **Given:** A running server with a handler instrumented to panic with a non-abort string value
- **When:** That endpoint is called
- **Then:** HTTP status is `500`
- **And:** The RFC 9457 body has `type` `INTERNAL`
- **And:** The `detail` does NOT expose internal stack trace or panic message to the caller
- **And:** The panic is logged server-side at ERROR level

### TC-I108: Panic with http.ErrAbortHandler is re-panicked (not converted to 500)

- **Requirement:** REQ-HTTP-070
- **Priority:** Medium
- **Type:** Integration
- **Given:** A running server with a handler instrumented to panic with `http.ErrAbortHandler`
- **When:** That endpoint is called
- **Then:** The connection is aborted (no response body written)
- **And:** The panic is NOT converted to an RFC 9457 error response (Go's `net/http` handles it natively)

### TC-I109: Panic after headers sent does not write a second response

- **Requirement:** REQ-HTTP-070
- **Priority:** Medium
- **Type:** Integration
- **Given:** A running server with a handler that writes the response header and then panics
- **When:** That endpoint is called
- **Then:** The response has whatever status code the handler wrote
- **And:** No duplicate or appended response body is written by the recovery middleware

---

## Coverage Matrix

| Requirement | Test Cases |
|-------------|------------|
| REQ-HTTP-010 | TC-I001, TC-I007 |
| REQ-HTTP-020 | TC-I001 |
| REQ-HTTP-030 | TC-I002 |
| REQ-HTTP-040 | TC-I002 |
| REQ-HTTP-050 | TC-I001 (via config), TC-U002, TC-U004, TC-I110, TC-I111 (E2E placeholder) |
| REQ-HTTP-060 | TC-I004, TC-I005 |
| REQ-HTTP-070 | TC-I107, TC-I108, TC-I109 |
| REQ-HTTP-080 | TC-I001 |
| REQ-HTTP-090 | TC-I008 |
| REQ-HTTP-091 | TC-I100 |
| REQ-HTTP-110 | TC-I003 |
| REQ-HLT-010 | TC-I077 |
| REQ-HLT-020 | TC-I077–I080 |
| REQ-HLT-030 | TC-I077 |
| REQ-HLT-040 | TC-I077 |
| REQ-HLT-050 | TC-I077 |
| REQ-HLT-060 | TC-I077–I079 |
| REQ-HLT-070 | TC-I041 |
| REQ-HLT-080 | TC-I081 |
| REQ-API-010 | TC-I006, TC-I009 |
| REQ-API-020 | TC-I009 |
| REQ-API-030 | TC-I009 |
| REQ-API-040 | TC-I026 |
| REQ-API-050 | TC-U012, TC-U047 |
| REQ-API-060 | TC-I009 |
| REQ-API-070 | TC-I009 |
| REQ-API-080 | TC-I026, TC-I069 |
| REQ-API-090 | TC-U014, TC-I009 |
| REQ-API-100 | TC-U055 |
| REQ-API-110 | TC-U054 |
| REQ-API-130 | TC-U056 |
| REQ-API-140 | TC-I009 |
| REQ-API-150 | TC-I029, TC-I030 |
| REQ-API-160 | TC-I031 |
| REQ-API-170 | TC-U017 |
| REQ-API-180 | TC-I033–I036 |
| REQ-API-190 | TC-I032 |
| REQ-API-200 | TC-I033, TC-I034 |
| REQ-API-210 | TC-I037 |
| REQ-API-220 | TC-I039 |
| REQ-API-230 | TC-I100–I103 |
| REQ-API-240 | TC-I103, TC-I104 |
| REQ-API-250 | TC-I105 |
| REQ-STR-010 | TC-I009, TC-I041, TC-I110, TC-I111 (E2E placeholder) |
| REQ-STR-020 | TC-I009 |
| REQ-STR-030 | TC-I026, TC-I069 |
| REQ-STR-040 | TC-I032, TC-I033, TC-I044 |
| REQ-STR-050 | TC-I031 |
| REQ-STR-060 | TC-I084 |
| REQ-STR-070 | TC-I037, TC-I039 |
| REQ-STR-080 | TC-I032, TC-I039, TC-I069 |
| REQ-CNPG-010 | TC-I009 |
| REQ-CNPG-020 | TC-I011 |
| REQ-CNPG-030 | TC-I043, TC-I044 |
| REQ-CNPG-040 | TC-I014, TC-I015 |
| REQ-CNPG-050 | TC-I012 |
| REQ-CNPG-060 | TC-I013 |
| REQ-CNPG-070 | TC-I086 |
| REQ-CNPG-080 | TC-I027, TC-I028 |
| REQ-CNPG-090 | TC-I016, TC-I019 |
| REQ-CNPG-100 | TC-I016 |
| REQ-CNPG-110 | TC-I017, TC-I018, TC-I036 |
| REQ-CNPG-120 | TC-I083 |
| REQ-CNPG-130 | TC-I020, TC-I021 |
| REQ-CNPG-140 | TC-I022, TC-I023 |
| REQ-CNPG-150 | TC-I024 |
| REQ-CNPG-160 | TC-I025 |
| REQ-CNPG-170 | TC-I037, TC-I040 |
| REQ-CNPG-180 | TC-I038 |
| REQ-CNPG-190 | TC-I069 |
| REQ-CNPG-200 | TC-I087 |
| REQ-MON-010 | TC-I045, TC-I073 |
| REQ-MON-020 | TC-I045 |
| REQ-MON-030 | TC-I042, TC-I043 |
| REQ-MON-040 | TC-I074 |
| REQ-MON-050 | TC-I046 |
| REQ-MON-060 | TC-I062–I064 |
| REQ-MON-070 | TC-I065 |
| REQ-MON-080 | (no dedicated integration test) |
| REQ-MON-090 | TC-I048 |
| REQ-MON-100 | TC-I050 |
| REQ-MON-110 | TC-I075, TC-I076 |
| REQ-MON-120 | (no dedicated integration test) |
| REQ-MON-130 | TC-I050 |
| REQ-MON-140 | TC-I046 |
| REQ-MON-145 | TC-I047 |
| REQ-MON-150 | TC-I047–I049 |
| REQ-MON-155 | TC-I048 |
| REQ-MON-160 | TC-I066, TC-I067 |
| REQ-MON-165 | TC-I068 |
| REQ-MON-170 | TC-I042–I044 |
| REQ-MON-175 | TC-I046 |
| REQ-MON-180 | TC-I112 (placeholder — see Notes) |
| REQ-MON-185 | TC-I082 |
| REQ-XC-ID-010 | TC-I009, TC-I010 |
| REQ-XC-ID-020 | TC-I069 |
| REQ-XC-ERR-010 | TC-I100, TC-I101 |
| REQ-XC-ERR-020 | TC-I100–I103 |
| REQ-XC-ERR-030 | TC-I102 |
| REQ-XC-ERR-040 | TC-I103, TC-I104 |
| REQ-XC-ERR-050 | TC-I105 |
| REQ-XC-ERR-055 | TC-I105 |
| REQ-XC-ERR-060 | TC-I106 |
| REQ-XC-ERR-070 | TC-I106 |
| REQ-XC-LBL-010 | TC-I010, TC-I029 |
| REQ-XC-LBL-020 | TC-I105 |
| REQ-XC-LOG-010 | TC-I004 |
| REQ-XC-LOG-020 | TC-I005 |
| REQ-XC-CFG-010 | TC-U002, TC-U004 |
| REQ-XC-CFG-020 | TC-U063 |
| REQ-XC-CFG-030 | TC-U082–TC-U085 |
| REQ-REG-010 | (no integration test — static code verification) |
| REQ-REG-020 | TC-I001 |

---

## 9 · E2E Authentication Placeholders

> **Suggested Ginkgo structure:** `Describe("E2E Authentication", Label("e2e"))`
>
> These tests require a real Kubernetes cluster (or kind/k3s) with the CNPG
> operator installed. They are tagged `e2e` and excluded from the standard
> `go test` run. They exist to document the coverage gap and serve as stubs
> for CI environments that can provision a cluster.

### TC-I110: SP authenticates to Kubernetes cluster via kubeconfig

- **Requirement:** REQ-HTTP-050 (server config), REQ-STR-010 (store interface)
- **Priority:** High
- **Type:** E2E (placeholder — requires real cluster)
- **Given:** A kubeconfig with valid credentials for a test cluster with CNPG installed
- **When:** The SP starts with `SP_K8S_KUBECONFIG` set to that kubeconfig path
- **Then:** `GET /api/v1alpha1/databases/health` returns `{"status":"healthy"}`
- **And:** `POST /api/v1alpha1/databases` successfully creates a CNPG Cluster resource
- **TODO:** Implement when a CI environment with a real Kubernetes cluster is available

### TC-I111: SP authenticates to Kubernetes using in-cluster service account

- **Requirement:** REQ-HTTP-050, REQ-STR-010
- **Priority:** High
- **Type:** E2E (placeholder — requires in-cluster deployment)
- **Given:** The SP is deployed as a Pod inside the target Kubernetes cluster with an appropriate ServiceAccount and RBAC bindings
- **When:** The SP starts without `SP_K8S_KUBECONFIG` (falls back to in-cluster config)
- **Then:** `GET /api/v1alpha1/databases/health` returns `{"status":"healthy"}`
- **And:** CRUD operations complete successfully against the in-cluster CNPG operator
- **TODO:** Implement when a CI environment with an in-cluster deployment is available

---

## Notes

- **Ginkgo BeforeSuite:** Set up embedded NATS server and base fake K8s client once per suite. Tear down in AfterSuite.
- **Ginkgo BeforeEach / AfterEach:** Reset fake K8s client state between tests to ensure isolation. Reset the NATS subscription listener.
- **Informer sync:** After adding objects to the fake K8s client in tests, call `cache.WaitForCacheSync` or use `Eventually(informer.HasSynced)` to prevent flakes.
- **Debounce timing:** Tests that exercise debounce (TC-I066–I068) use `SP_MONITOR_DEBOUNCE_WINDOW="100ms"` so they run fast. Always add a generous buffer (`Eventually(...).Within(500ms)`) after the window.
- **Connection details:** TC-I033 requires a CNPG Cluster with `readyInstances=1`. The CNPG API's `status.readyInstances` field must be set on the fake object directly since the fake K8s client does not run the CNPG operator.
- **CNPG CRD objects:** The CNPG Cluster CRD type is not a core K8s type; use `k8s.io/client-go/dynamic/fake` for CRD operations alongside `kubernetes/fake` for core types (Pods, PVCs, Services, Secrets).
- **Utility transitive coverage:** Utility TC-IDs (TC-U024–TC-U042, TC-U052–TC-U057, TC-U072, TC-U094–TC-U096) are referenced via `Transitively covers:` lines and have no dedicated integration `It` blocks.
- **TC-I082:** Covers initial status sync on startup (REQ-MON-185); see section 6 · Status Monitoring.
- **TC-I112 (NATS reconnect, placeholder):** Requires a controllable embedded NATS server. Extend `BeforeSuite` to expose a `StopNATS()` / `StartNATS()` handle using `natsserver/server` directly. Until then this test is tagged `pending` and excluded from CI.
- **Multi-error validation (TC-I105–I106):** These tests require the server to be wired end-to-end so that two independent handler-level validation checks run and aggregate. The `errors` array and `pointer` field are extensions on the RFC 9457 `Error` schema as defined in the OpenAPI spec.
