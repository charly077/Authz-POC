# Contributing Guide

## Development Workflow

1. Clone the repository
2. Copy `.env.example` to `.env` and fill in required values (see [Environment Variables](#environment-variables))
3. Start the stack: `podman compose up --build -d`
4. Make changes
5. Rebuild affected services: `podman compose up --build -d <service-name>`
6. Run tests before committing (see [Testing](#testing))
7. Commit using conventional commits: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`

## Prerequisites

- Podman (or Docker)
- Go 1.21+ (for test-app development)
- Node.js 18+ (for ai-manager development)
- Google Gemini API Key (for AI Manager features)

## Project Structure

```
├── ai-manager/          # AI Authorization Manager (Node.js/Express)
├── docs/                # Project documentation
├── infra/
│   ├── envoy/           # Envoy proxy configuration
│   ├── grafana/         # Grafana dashboards and provisioning
│   ├── keycloak/        # Keycloak realm config and themes
│   ├── loki/            # Loki log aggregation config
│   ├── opa/             # OPA policies (.rego) and config
│   ├── openfga/         # OpenFGA model bootstrap (init.js)
│   ├── postgres/        # Database init scripts
│   └── promtail/        # Promtail log collector config
├── test-app/            # Citizen Mandate System (Go)
│   ├── internal/
│   │   ├── audit/       # Audit logging client
│   │   ├── config/      # Global configuration
│   │   ├── fga/         # OpenFGA API client
│   │   ├── handlers/    # HTTP handlers (dossiers, orgs, guardianships)
│   │   ├── httputil/    # HTTP utilities
│   │   ├── store/       # Data persistence and types
│   │   └── templates/   # HTML templates
│   ├── go.mod
│   └── main.go          # Routes and server setup
├── docker-compose.yml
├── .env.example
└── README.md
```

## Scripts Reference

### ai-manager (Node.js)

| Script | Command | Description |
|--------|---------|-------------|
| `start` | `node server.js` | Start the AI Manager Express server |

### test-app (Go)

| Command | Description |
|---------|-------------|
| `go build ./...` | Compile all packages |
| `go test ./...` | Run all tests |
| `go test ./... -v` | Run tests with verbose output |
| `go test ./... -race` | Run tests with race detector |
| `go test -cover ./...` | Run tests with coverage report |

### Infrastructure

| Command | Description |
|---------|-------------|
| `podman compose up --build -d` | Build and start all services |
| `podman compose down` | Stop all services |
| `podman compose down -v` | Stop and remove volumes (clean reset) |
| `podman compose logs -f <service>` | Follow logs for a service |
| `podman compose restart <service>` | Restart a specific service |

## Environment Variables

All variables are defined in `.env.example`. Copy to `.env` before first run.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GEMINI_API_KEY` | **Yes** | _(none)_ | Google Gemini API key for AI Manager. Get one at https://aistudio.google.com/apikey |
| `KEYCLOAK_ADMIN` | No | `admin` | Keycloak admin console username |
| `KEYCLOAK_ADMIN_PASSWORD` | No | `admin` | Keycloak admin console password |
| `POSTGRES_USER` | No | `admin` | PostgreSQL username (shared by Keycloak and OpenFGA) |
| `POSTGRES_PASSWORD` | No | `password` | PostgreSQL password |
| `ENVOY_TOKEN_SECRET` | No | `envoy-secret` | Envoy OAuth2 token secret (must match Keycloak client config) |
| `ENVOY_HMAC_SECRET` | No | `change-me-to-a-long-random-string` | Envoy HMAC signing secret for session cookies |
| `AI_MANAGER_CLIENT_SECRET` | No | `ai-manager-secret` | AI Manager OIDC client secret (must match Keycloak) |
| `AI_MANAGER_ADMIN_PASSWORD` | No | `admin` | AI Manager admin user password in Keycloak |
| `SESSION_SECRET` | No | `change-me-to-a-random-string` | Express session signing secret |
| `GF_SECURITY_ADMIN_PASSWORD` | No | `admin` | Grafana admin password |
| `GRAFANA_CLIENT_SECRET` | No | `grafana-secret` | Grafana Keycloak OAuth client secret |

## Testing

### Go (test-app)

Run all tests:
```bash
cd test-app
go test ./...
```

Run with verbose output:
```bash
go test ./... -v
```

Run with race detection:
```bash
go test ./... -race
```

### Test Organization

Tests are organized by package:

- `internal/handlers/` — Handler tests (HTTP endpoint behavior, mock FGA server)
- `internal/store/` — Store tests (persistence, tuple rehydration)
- `internal/httputil/` — HTTP utility tests
- `internal/templates/` — Template rendering tests

### Writing Tests

Follow existing patterns:
1. Use `setupFGA(t, handler)` to create a mock OpenFGA server
2. Use `resetStore(t)` to get a clean data store
3. Both return cleanup functions — always `defer` them
4. Test HTTP handlers by creating `httptest.NewRecorder()` and `httptest.NewRequest()`

## Service Ports

| Port | Service | Description |
|------|---------|-------------|
| 8000 | Envoy | Public entry point (all traffic) |
| 8081 | OpenFGA | HTTP API |
| 8082 | OpenFGA | gRPC API |
| 3001 | OpenFGA | Playground UI |
| 8181 | OPA | Policy API |
| 9191 | OPA | Envoy ext_authz gRPC |
| 9901 | Envoy | Admin interface |

Internal services (not exposed):
- Keycloak: 8080 (accessed via Envoy at `/login`)
- test-app: 3000 (accessed via Envoy)
- ai-manager: 5000 (accessed via Envoy at `/manager`)
- Postgres: 5432
- Loki: 3100
- Grafana: 3000 (accessed via Envoy at `/grafana`)
