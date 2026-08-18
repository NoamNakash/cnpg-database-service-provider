# Design Decisions: CNPG Database Service Provider

This document records architectural and design decisions for the CNPG Database
Service Provider, referenced by ID (`DD-NNN`) from the specs in `.ai/specs/`.
New decisions are appended here as implementation surfaces them, so this file
stays open across milestones rather than being tied to any single spec
document's lifecycle.

**Related Specs:** `.ai/specs/cnpg-database-sp.spec.md`

---

## DD-010: CNPG Clusters as the database abstraction unit

**Decision:** Each DCM database maps 1:1 to a CNPG `Cluster` CRD, not to raw
Kubernetes `StatefulSet` resources.

**Rationale:** CNPG provides built-in PostgreSQL lifecycle management (failover,
replication, backup hooks, rolling upgrades) that would require significant
reimplementation on top of a raw StatefulSet. CNPG is the authoritative tool
for PostgreSQL on Kubernetes; using it as the backing abstraction ensures the
SP benefits from its maturity without re-engineering database operations.

**Trade-off:** The SP is tightly coupled to the CNPG operator being present in
the cluster. This is explicitly reflected in the health check (see DD-050).

**Applies to:** REQ-CNPG-010

---

## DD-020: Introducing DELETING as an intermediate status

**Decision:** The SP introduces a `DELETING` DCM status published via
CloudEvent when a delete is initiated. This status bridges the gap between the
`DELETE` API call returning 204 and the CNPG Cluster fully disappearing from
Kubernetes.

**Rationale:** Without `DELETING`, DCM consumers would observe a resource
going from RUNNING directly to DELETED (or would miss the deletion entirely if
DELETED is only inferred from the Cluster disappearing). The intermediate status
lets consumers react to deletion being in progress.

**Implication:** `DELETING` is not yet in the `openapi.yaml` `DatabaseStatus`
enum and MUST be added before Topic 6 is implemented.

**Applies to:** REQ-MON-090, REQ-MON-155

---

## DD-030: Single configured namespace for all resources

**Decision:** All CNPG Clusters, Services, and Secrets managed by this SP are
created in a single Kubernetes namespace configured at startup
(`SP_K8S_NAMESPACE`).

**Rationale:** Multi-namespace isolation is a future concern. A single namespace
simplifies informer setup (all three informers watch one namespace), avoids
cross-namespace RBAC complexity, and matches the v1 scope. Namespace isolation
can be layered via separate SP instances if required.

**Trade-off:** All databases for a given SP instance share a namespace. Noisy
neighbor scenarios at the namespace level are out of scope for v1. This also
means per-tenant resource quota's are not applicable in Kubernetes level
currently.

**Applies to:** REQ-CNPG-010, REQ-MON-010

---

## DD-040: Repository pattern for store abstraction

**Decision:** All Kubernetes and CNPG interactions are encapsulated behind a
`DatabaseRepository` interface. API handlers and the health service depend
only on this interface, not on any Kubernetes client directly.

**Rationale:** This decoupling allows handlers to be tested against a mock
store without a real Kubernetes cluster. It also ensures a single seam for
injecting the CNPG implementation versus future alternative backends.
`CheckHealth` is part of the interface so the health endpoint remains
consistent with the same boundary rule.

**Applies to:** REQ-STR-010, REQ-HLT-070

---

## DD-050: Health check verifies CNPG operator presence

**Decision:** The health endpoint checks both (1) Kubernetes API server
reachability and (2) CNPG operator availability (via CRD discovery or
operator deployment check). The check should preformed via unauthenticated
endpoints to minimize requests to the Kubernetes API

**Rationale:** A healthy K8s API server alone is insufficient — if the CNPG
operator is absent, all create operations will silently create `Cluster`
resources that will never reconcile. Checking CNPG presence surfaces this
early via DCM's health polling.

**Trade-off:** Adds one extra API call to the health path. The check MUST be
lightweight (no full CRD introspection, just a presence check).

**Applies to:** REQ-HLT-030, REQ-HLT-050

---

## DD-060: Disabling CNPG ro and r services

**Decision:** The SP explicitly disables the CNPG-default `ro` (read-only
replica endpoint) and `r` (any-instance round-robin) services, keeping only
the `rw` service.

**Rationale:** DCM exposes services through the `services` array. Surfacing
all three CNPG services would confuse consumers, as the semantics of `ro` and
`r` are PostgreSQL-specific and not part of the DCM service model. The `rw`
service is the primary connection target and cannot be disabled.

**Applies to:** REQ-CNPG-090, REQ-CNPG-100

---

## DD-070: External visibility via a second service

**Decision:** When `network.visibility=external`, the SP creates an additional
service (LoadBalancer or NodePort) rather than changing the type of the `rw`
ClusterIP service.

**Rationale:** CNPG does not support changing the `rw` service type; it
manages this service internally. Creating a second service of the external type
is the CNPG-idiomatic approach. Both services are reported in the `services`
array.

**Applies to:** REQ-CNPG-110

---

## DD-080: generateName for Kubernetes resource naming

**Decision:** Kubernetes resource names are server-assigned via
`metadata.generateName`, using the client-supplied `metadata.name` as the
prefix. Resources are always looked up by the `dcm.project/dcm-instance-id`
label, not by name.

**Rationale:** Kubernetes name uniqueness within a namespace would force the
SP to enforce name uniqueness, duplicating what the `id` field already
provides. Using `generateName` decouples the human-readable name from the
actual K8s resource name. Conflict detection is based solely on the
`dcm-instance-id` label.

**Implication:** Two databases can have the same `metadata.name` (e.g.,
`"my-db"`) as long as their `id` values differ.

**Applies to:** REQ-CNPG-020, REQ-CNPG-030, REQ-XC-ID-010, REQ-XC-ID-020

---

## DD-090: Memory unit conversion (MB→Mi, GB→Gi, TB→Ti)

**Decision:** The API schema accepts memory values in SI units (MB, GB, TB)
and binary-prefix aliases (MiB, GiB, TiB). The store converts all values to
Kubernetes binary suffixes (Mi, Gi, Ti) before setting container resource
limits.

**Rationale:** Kubernetes container resources use binary-prefix suffixes
(Mi = 2^20 bytes). DCM's API schema uses the more user-friendly MB/GB
notation. The SP performs this conversion at the store layer, keeping the
API contract clean while ensuring Kubernetes gets correctly formatted values.

**Applies to:** REQ-CNPG-060

---

## DD-100: Three SharedIndexInformers for layered status

**Decision:** Three separate `SharedIndexInformer` instances are used —
one each for `Cluster`, `Pod`, and `PVC` — rather than a single informer
or a polling approach.

**Rationale:** Informers provide low-latency event-driven updates without
polling loops. Using three separate informers allows independent label selectors
for each resource type (e.g., the Pod informer adds `cnpg.io/podRole=instance`
to its selector). Shared informers are cache-efficient when multiple controllers
use the same list/watch stream.

**Applies to:** REQ-MON-010

---

## DD-110: DELETING-before-DELETED sequencing

**Decision:** The delete flow is: (1) publish DELETING CloudEvent, (2) delete
the Cluster, (3) wait for informers to detect Cluster removal and publish DELETED.
The DELETING event is emitted synchronously by the delete handler, not by
the informer reconciler.

**Rationale:** The informer will eventually observe the Cluster's
`deletionTimestamp` and emit another DELETING event, but the authoritative
DELETING signal is the API call itself. Emitting it synchronously ensures DCM
sees the transition immediately, even before the Cluster controller processes
the deletion.

**Applies to:** REQ-MON-090, REQ-MON-100, REQ-MON-155

---

## DD-120: Instance ID and status in CloudEvent data payload

**Decision:** The CloudEvent `data` payload includes `id` (DCM instance ID),
`status` (DCM status string), and `message` (human-readable description).
The DCM instance ID is also derivable from the resource label, but is
repeated in the data payload for consumer convenience.

**Rationale:** CloudEvent consumers should not need to parse `source` or
`subject` attributes to determine which database changed. Embedding `id`
directly in the data payload makes event routing straightforward.

**Applies to:** REQ-MON-145, REQ-MON-150

---

## DD-130: DCM labels as the sole ownership mechanism

**Decision:** Three labels — `dcm.project/managed-by=dcm`,
`dcm.project/dcm-instance-id={id}`, and `dcm.project/dcm-service-type=database`
— are applied to all K8s resources. These labels are also used as informer
selectors.

**Rationale:** Labels are the standard Kubernetes ownership convention.
Using them as both metadata and informer selectors ensures the SP only
processes resources it owns. The three-label combination is sufficient for
filtering without requiring additional annotations or owner references.

**Implication:** Client-supplied `metadata.labels` that collide with these
keys MUST be rejected at the API layer (see SC-004).

**Applies to:** REQ-XC-LBL-010, REQ-MON-020, REQ-MON-170

---

## DD-140: Unlimited NATS reconnection

**Decision:** The NATS client MUST be configured with unlimited reconnection
attempts (`nats.MaxReconnects(-1)`) and MUST log disconnect and reconnect events.

**Rationale:** NATS is a messaging infrastructure dependency that may
temporarily become unavailable during maintenance or network partitions. The
SP should not exit on a NATS disconnect — doing so would take down the HTTP API
unnecessarily. Status events that cannot be published during an outage are
effectively lost (no buffering), but the next status change will publish
successfully once reconnected.

**Trade-off:** Status events are not buffered during NATS outages. DCM must
tolerate eventual status delivery.

**Applies to:** REQ-MON-180

---

## DD-150: connection_details is output-only (AEP-203)

**Decision:** The `connection_details` field is strictly output-only. It is
never accepted in request bodies and MUST be empty string in all responses
except when the database is in RUNNING status.

**Rationale:** `connection_details` contains credentials (host, port,
credentials). These are derived from live Kubernetes Secret resources managed
by CNPG and cannot be meaningfully supplied by the client. Following AEP-203,
input-only and output-only fields have strict directionality.

**Applies to:** REQ-API-140, REQ-API-200

---

## DD-160: provider_hints.postgres.initdb is input-only (AEP-203)

**Decision:** The `provider_hints.postgres.initdb` block is input-only. It is
consumed at create time to configure CNPG bootstrap, but is never stored or
returned in API responses.

**Rationale:** Returning initdb configuration in responses would expose
password values in plaintext. Following AEP-203, input-only fields are
accepted in requests but omitted from all responses.

**Applies to:** REQ-CNPG-140, REQ-CNPG-150
