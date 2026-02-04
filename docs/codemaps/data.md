# Data Codemap

> **Freshness:** 2026-02-04 | **Version:** 1.0.0

## Overview

Data models span three layers:
1. **Application Data** (Go structs, persisted JSON)
2. **OpenFGA Tuples** (relationship storage)
3. **Keycloak Users** (identity)

---

## Application Data Models

### Location: `test-app/internal/store/types.go`

### Dossier

```go
type Dossier struct {
    Title        string     `json:"title"`
    Content      string     `json:"content"`
    Type         string     `json:"type"`      // "tax", "health", "general"
    Owner        string     `json:"owner"`
    Relations    []Relation `json:"relations"`
    OrgId        string     `json:"orgId,omitempty"`
    Public       bool       `json:"public,omitempty"`
    BlockedUsers []string   `json:"blockedUsers,omitempty"`
}

type Relation struct {
    User     string `json:"user"`
    Relation string `json:"relation"`  // "mandate_holder"
}
```

### Organization

```go
type Organization struct {
    Name    string   `json:"name"`
    Members []string `json:"members"`
    Admins  []string `json:"admins"`
}
```

### GuardianshipRequest

```go
type GuardianshipRequest struct {
    ID     string `json:"id"`
    From   string `json:"from"`
    To     string `json:"to"`
    Status string `json:"status"`  // "pending", "accepted", "denied", "removed"
}
```

### DataStore

```go
type DataStore struct {
    Dossiers             map[string]Dossier            `json:"dossiers"`
    Guardians            map[string][]string           `json:"guardians"`    // userId -> [guardianIds]
    GuardianshipRequests []GuardianshipRequest         `json:"guardianshipRequests"`
    Organizations        map[string]Organization       `json:"organizations"`
    Users                []string                      `json:"users"`
}
```

### Persistence

**File:** `/data/dossiers.json` (Docker volume: `test_app_data`)

```json
{
  "dossiers": {
    "doc1": {
      "title": "Tax Return 2024",
      "content": "...",
      "type": "tax",
      "owner": "alice",
      "relations": [{"user": "bob", "relation": "mandate_holder"}],
      "orgId": "bosa",
      "public": false,
      "blockedUsers": []
    }
  },
  "guardians": {
    "alice": ["bob"]
  },
  "guardianshipRequests": [
    {"id": "req1", "from": "bob", "to": "alice", "status": "accepted"}
  ],
  "organizations": {
    "bosa": {
      "name": "BOSA",
      "members": ["alice", "bob"],
      "admins": ["alice"]
    }
  },
  "users": ["alice", "bob", "charlie"]
}
```

---

## OpenFGA Authorization Model

### Location: `infra/openfga/init.js`

### Type Definitions

```yaml
schema_version: "1.1"

type user
  relations:
    guardian: [user]    # user:bob guardian user:alice

type organization
  relations:
    member: [user]      # user:alice member organization:bosa
    admin: [user]       # user:alice admin organization:bosa
    can_manage: admin   # computed: can_manage = admin

type dossier
  relations:
    owner: [user]              # user:alice owner dossier:doc1
    mandate_holder: [user]     # user:bob mandate_holder dossier:doc1
    org_parent: [organization] # organization:bosa org_parent dossier:doc1
    blocked: [user]            # user:charlie blocked dossier:doc1
    public: [user:*]           # user:* public dossier:doc1 (wildcard)

    # Computed relations
    can_view:
      - owner
      - mandate_holder
      - owner->guardian         # tupleToUserset
      - org_parent->member      # tupleToUserset
      - public

    viewer: can_view but not blocked  # difference
    editor: owner | mandate_holder
```

### Tuple Examples

```
# Direct ownership
user:alice  owner  dossier:doc1

# Mandate delegation
user:bob  mandate_holder  dossier:doc1

# Guardianship (user-to-user)
user:bob  guardian  user:alice

# Organization membership
user:alice  member  organization:bosa
user:alice  admin   organization:bosa

# Dossier-to-org assignment
organization:bosa  org_parent  dossier:doc1

# Public access (wildcard)
user:*  public  dossier:doc1

# User blocking
user:charlie  blocked  dossier:doc1
```

### Tuple Storage

**File:** `/shared/openfga-store.json` (Docker volume: `openfga_config`)

```json
{
  "store_id": "01HXYZ...",
  "model_id": "01HXYZ..."
}
```

**Database:** PostgreSQL `openfga` database (managed by OpenFGA)

---

## Keycloak Identity Model

### Location: `infra/keycloak/realm.json`, `infra/keycloak/ai-manager-realm.json`

### Realms

| Realm | Purpose |
|-------|---------|
| AuthorizationRealm | test-app users (alice, bob) |
| AIManagerRealm | ai-manager users + Grafana SSO |

### Users (AuthorizationRealm)

```json
{
  "username": "alice",
  "enabled": true,
  "credentials": [{"type": "password", "value": "alice"}],
  "realmRoles": ["default-roles-authorizationrealm"]
}
```

### Users (AIManagerRealm)

```json
{
  "username": "admin",
  "enabled": true,
  "credentials": [{"type": "password", "value": "${AI_MANAGER_ADMIN_PASSWORD}"}],
  "realmRoles": ["ai-admin"]
}
```

### Roles

| Realm | Role | Purpose |
|-------|------|---------|
| AIManagerRealm | ai-admin | Admin operations in ai-manager |

### Clients

| Client | Realm | Purpose |
|--------|-------|---------|
| envoy | AuthorizationRealm | OIDC for test-app |
| ai-manager | AIManagerRealm | OIDC for ai-manager |
| grafana | AIManagerRealm | SSO for Grafana |

---

## API Request/Response Schemas

### Dossier Create

```typescript
// Request
POST /api/dossiers/create
{
  title: string,
  content: string,
  type: "tax" | "health" | "general",
  orgId?: string,
  public?: boolean
}

// Response
{
  id: string,
  success: true
}
```

### Guardianship Request

```typescript
// Request
POST /api/dossiers/guardianships/request
{
  to: string  // target user
}

// Response
{
  request: GuardianshipRequest
}
```

### OpenFGA Tuple

```typescript
// Request
POST /api/openfga/tuples
{
  user: string,      // "user:alice"
  relation: string,  // "owner"
  object: string     // "dossier:doc1"
}

// Response
{ success: true }
```

### Permission Check

```typescript
// Request
GET /api/openfga/check?user=user:alice&relation=viewer&object=dossier:doc1

// Response
{
  allowed: boolean
}
```

---

## Data Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │────►│  test-app   │────►│   Store     │
│  (Browser)  │     │   (Go)      │     │  (JSON)     │
└─────────────┘     └──────┬──────┘     └─────────────┘
                          │
                          ▼
                   ┌─────────────┐
                   │   OpenFGA   │
                   │  (Tuples)   │
                   └─────────────┘

On startup:
1. Load Store from /data/dossiers.json
2. RehydrateTuples() → Write all tuples to OpenFGA
3. Ready to serve requests

On data change:
1. Update Store in memory
2. Write tuples to OpenFGA
3. Save Store to disk
```

---

## Audit Log Schema

### Location: In-memory in ai-manager (not persisted)

```typescript
interface AuditEntry {
  timestamp: string,      // ISO 8601
  action: string,         // "write_tuple", "delete_tuple", etc.
  user: string,           // "alice"
  details: {
    tuples?: TupleKey[],
    dossier?: string,
    organization?: string,
    [key: string]: any
  }
}
```

### Audit Actions

| Action | Source | Description |
|--------|--------|-------------|
| write_tuple | test-app | Tuple written to OpenFGA |
| delete_tuple | test-app | Tuple deleted from OpenFGA |
| create_dossier | test-app | New dossier created |
| update_dossier | test-app | Dossier modified |
| delete_dossier | test-app | Dossier removed |
| create_org | test-app | Organization created |
| add_member | test-app | Member added to org |
| remove_member | test-app | Member removed from org |
| add_admin | test-app | User promoted to admin |
| remove_admin | test-app | User demoted from admin |
