# CloudNativePG Database Service Provider

A [DCM](https://github.com/dcm-project) service provider for managing
PostgreSQL databases on Kubernetes clusters using `Cluster` resources
controlled by the [CloudNativePG](https://cloudnative-pg.io/) operator.

## Features

- Database lifecycle management - CREATE, READ and DELETE operations
- Kubernetes-native using cnpg - each database maps to a cnpg Cluster resource;
  ports are exposed via Services managed by the Cluster resource (NodePort or
  LoadBalancer)
- Resource constraints - CPU (cores) and memory (MB/GB/TB) with min/max
  boundaries; storage (MB/GB/TB) with set amounts
- Service exposure - database can be exposed with `internal` (ClusterIP) or
  `external` (NodePort/LoadBalancer) visibility
- Status monitoring - watches Kubernetes resources and publishes status change
  events (PENDING, FAILED, UNKNOWN, DELETED) via NATS using 
  [Cloud Events](https://cloudevents.io/) v1.0 on NATS subject `dcm.database`
- Auto-registration - registers itself with the DCM control plane on startup,
  with exponential backoff retry.
- AEP-compliant API - follows [API Enhancement Proposals](https://aep.dev)
  standards; request validation is enforced via embedded OpenAPI spec
- RFC 9457 errors - all error responses use the Problem Details format

## API Endpoints

Contract: `api/v1alpha1/openapi.yaml`
Base path: `/api/v1alpha1/databases`

### Health

| Method | Path                             | Description                                           |
| ------ | -------------------------------- | ----------------------------------------------------- |
| `GET`  | `/api/v1alpha1/databases/health` | Returns health status, uptime (seconds), and version. |

Response example:

```json
{
    "type": "cnpg-database-service-provider.dcm.io/health",
    "status": "healthy",
    "path": "health",
    "version": "0.0.1-dev",
    "uptime": 3600
}
```

### Database Operations

| Method   | Path                                    | Description                                                                                                                     |
| -------- | --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `GET`    | `/api/v1alpha1/databases`               | List databases. Supports `max_page_size` (1-1000, default 50) and `page_token` query parameters.                                |
| `POST`   | `/api/v1alpha1/databases`               | Create a database. Accepts optional `id` query parameter in the [AEP-122 Resource ID](https://aep.dev/122/#resource-id) format. |
| `GET`    | `/api/v1alpha1/databases/{database_id}` | Get a database by ID.                                                                                                           |
| `DELETE` | `/api/v1alpha1/databases/{database_id}` | Delete a database. Returns `204 No Content`.                                                                                    |

All error responses use [RFC 9457](https://www.rfc-editor.org/info/rfc9457/)
Problem Details with types:

- https://dcm-project.github.io/problems/invalid-argument
- https://dcm-project.github.io/problems/not-found
- https://dcm-project.github.io/problems/already-exists
- https://dcm-project.github.io/problems/permission-denied
- https://dcm-project.github.io/problems/unauthenticated
- https://dcm-project.github.io/problems/internal
- https://dcm-project.github.io/problems/unavailable

## Development

### Prerequisites

- Go 1.25.5+
- `make`
- Access to a Kubernetes cluster with CloudNativePG installed

### Build and Run

```bash
make build  # Build binary to bin/cnpg-database-service-provider
make run    # Run via go run
```

### Test

```bash
make test       # Run all tests (Ginkgo v2, race detector)
make test-cover # Run tests with coverage report (coverprofile.out
```

### Linting and Checks

```bash
make lint   # Run golangci-lint
make check  # fmt + vet + lint + test (full validation)
```

### Code Generation

```bash
make generate-api         # Regenerate types, server, and client from OpenAPI
make check-generate-api # Verify generated code is up to date (CI)
make check-aep            # Validate OpenAPI against AEP (requires spectral)
```

Generated files (do not edit manually):

- `api/v1alpha1/types.gen.go` — data models
- `api/v1alpha1/spec.gen.go` — embedded OpenAPI spec
- `internal/api/server/server.gen.go` — Chi router and strict server interface
- `pkg/client/client.gen.go` — HTTP client

## Project Structure

```
.
├── api/v1alpha1                        # OpenAPI spec and generated types
├── cmd/cnpg-database-service-provider  # Entry point (bootstrap stub)
├── internal/api/server                 # Generated strict server interface
├── pkg/client/                         # Generated HTTP client
└── Makefile
```

## License

Apache License 2.0 see [LICENSE](LICENSE)
