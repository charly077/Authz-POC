# Operations Runbook

## Deployment

### First-Time Setup

```bash
# 1. Clone and configure
cp .env.example .env
# Edit .env — set GEMINI_API_KEY at minimum

# 2. Build and start everything
podman compose up --build -d

# 3. Verify all services are running
podman compose ps
```

Wait for all services to become healthy. OpenFGA init takes ~30s to bootstrap (waits for OpenFGA server, then creates store + auth model).

### Updating the Stack

```bash
# Rebuild and restart changed services
podman compose up --build -d

# Or rebuild a specific service
podman compose up --build -d test-app
```

### Clean Reset (New OpenFGA Model)

When the OpenFGA authorization model changes (e.g., `infra/openfga/init.js`), you must do a clean reset:

```bash
podman compose down -v
podman compose up --build -d
```

The `-v` flag removes all volumes including the OpenFGA store. The `openfga-init` container will recreate the store with the new model on startup.

### Synology NAS Deployment

See `README.md` for detailed Synology-specific instructions. Key differences:
- Use `docker compose` instead of `podman compose`
- Set `EXTERNAL_URL` to `http://<NAS_IP>:8000`
- Update Keycloak redirect URIs for the NAS IP

## Service Dependency Order

```
postgres
├── keycloak
├── openfga-migrate → openfga
│                     ├── openfga-init → (writes config to shared volume)
│                     └── test-app (reads shared volume for FGA config)
├── opa
└── envoy (routes to: test-app, keycloak, ai-manager, grafana, opa)

loki
├── promtail
└── grafana
```

## Monitoring and Logs

### Grafana

Access at `http://localhost:8000/grafana/` (login with Keycloak SSO or admin/`GF_SECURITY_ADMIN_PASSWORD`).

Pre-provisioned dashboards and Loki data source are loaded from `infra/grafana/provisioning/`.

### Viewing Logs

```bash
# All services
podman compose logs -f

# Specific service
podman compose logs -f test-app
podman compose logs -f envoy
podman compose logs -f openfga
podman compose logs -f keycloak

# Last 100 lines
podman compose logs --tail=100 test-app
```

### Key Log Patterns

| Pattern | Service | Meaning |
|---------|---------|---------|
| `OpenFGA is ready` | openfga-init | OpenFGA bootstrap complete |
| `Config written to /shared/openfga-store.json` | openfga-init | Store ID and model ID persisted |
| `Loaded OpenFGA config: store=... model=...` | test-app | test-app connected to OpenFGA |
| `Rehydrated N tuples from persisted data` | test-app | Tuple state restored after restart |
| `Waiting for OpenFGA config` | test-app | Still waiting for openfga-init (normal at startup) |
| `WARNING: Could not load OpenFGA config` | test-app | OpenFGA init failed — check openfga-init logs |

### OpenFGA Debug

View all stored tuples:
```bash
curl -s http://localhost:8081/stores/<STORE_ID>/read \
  -H 'Content-Type: application/json' \
  -d '{}' | jq
```

Or use the app's debug endpoint:
```bash
curl -s http://localhost:8000/api/dossiers/debug/tuples \
  -H 'x-current-user: alice' | jq
```

Or use the OpenFGA Playground at `http://localhost:3001`.

## Common Issues and Fixes

### OpenFGA not ready (503 responses)

**Symptom:** API returns `{"error":"OpenFGA not ready"}`.

**Cause:** The test-app hasn't loaded FGA config yet. The `openfga-init` container may still be running or failed.

**Fix:**
```bash
# Check openfga-init status
podman compose logs openfga-init

# If it failed, restart it
podman compose restart openfga-init

# Or do a clean reset
podman compose down -v && podman compose up --build -d
```

### Keycloak login redirects fail

**Symptom:** After login, redirected to wrong URL or get CORS errors.

**Cause:** `EXTERNAL_URL` mismatch between Envoy, test-app, and Keycloak redirect URIs.

**Fix:**
1. Ensure `EXTERNAL_URL` is set consistently in `docker-compose.yml` for `envoy` and `test-app`
2. Update `infra/keycloak/realm.json` redirect URIs to match
3. Restart: `podman compose restart keycloak envoy test-app`

### Tuples not persisted after restart

**Symptom:** Dossier data exists but OpenFGA returns empty results.

**Cause:** The `test-app` rehydrates tuples on startup from `/data/dossiers.json`. If the volume was removed, data is lost.

**Fix:** Data is stored in the `test_app_data` volume. If volume was removed (`-v` flag), all dossier data is gone. Recreate through the UI.

### OPA authorization failures

**Symptom:** Getting 403 on pages that should be accessible.

**Cause:** OPA policies may be incorrect or OPA can't reach Keycloak for JWKS.

**Fix:**
```bash
# Check OPA logs
podman compose logs opa

# Verify OPA policies are loaded
curl -s http://localhost:8181/v1/policies | jq '.result[].id'

# Test a policy decision manually
curl -s http://localhost:8181/v1/data/envoy/authz/allow \
  -H 'Content-Type: application/json' \
  -d '{"input":{}}' | jq
```

### Port conflicts

**Symptom:** Container fails to start with "port already in use" error.

**Fix:** Check which process is using the port and either stop it or remap in `docker-compose.yml`:
```bash
lsof -i :8000  # Check port 8000
```

### Database issues

**Symptom:** Keycloak or OpenFGA can't connect to Postgres.

**Fix:**
```bash
# Check postgres is running
podman compose ps postgres

# Check postgres logs
podman compose logs postgres

# If corrupted, reset
podman compose down -v
podman compose up --build -d
```

## Rollback Procedures

### Rolling Back Code Changes

```bash
# Revert to previous commit
git log --oneline -5  # Find the commit to revert to
git revert <commit-hash>

# Rebuild and restart
podman compose up --build -d
```

### Rolling Back OpenFGA Model Changes

OpenFGA model changes require a clean reset since they're applied at init time:

```bash
# Revert the model change
git checkout <previous-commit> -- infra/openfga/init.js

# Clean reset
podman compose down -v
podman compose up --build -d
```

**Warning:** This destroys all persisted dossier data and OpenFGA tuples. Export important data first.

### Rolling Back Keycloak Realm Changes

```bash
# Revert realm config
git checkout <previous-commit> -- infra/keycloak/realm.json

# Restart Keycloak (realm is re-imported on start with OVERWRITE_EXISTING)
podman compose restart keycloak
```

### Emergency: Restore to Known-Good State

```bash
# Full clean reset
podman compose down -v
git checkout main
podman compose up --build -d
```

## Health Checks

| Endpoint | Expected | Service |
|----------|----------|---------|
| `http://localhost:8000/public` | 200 HTML page | Envoy + test-app |
| `http://localhost:8000/api/health` | `{"status":"healthy","fgaReady":true}` | test-app |
| `http://localhost:8000/api/dossiers/status` | `{"ready":true,"storeId":"..."}` | test-app + OpenFGA |
| `http://localhost:8081/healthz` | 200 | OpenFGA |
| `http://localhost:8181/health` | 200 | OPA |
| `http://localhost:9901/ready` | 200 | Envoy admin |
