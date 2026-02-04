# Backend Codemap

> **Freshness:** 2026-02-04 | **Version:** 1.0.0

## Overview

Two backend services: **test-app** (Go) and **ai-manager** (Node.js)

## test-app (Go)

### Directory Structure

```
test-app/
├── main.go                    # Server entry, routes, config loading
├── go.mod                     # Dependencies (stdlib only)
├── Dockerfile                 # Multi-stage build
└── internal/
    ├── audit/
    │   └── client.go          # Audit event sender
    ├── config/
    │   └── config.go          # Global config vars
    ├── fga/
    │   └── client.go          # OpenFGA API client
    ├── handlers/
    │   ├── dossiers.go        # Dossier CRUD + ReBAC operations
    │   ├── guardianships.go   # Guardianship workflow
    │   ├── organizations.go   # Organization management
    │   └── debug.go           # Debug endpoints
    ├── httputil/
    │   └── httputil.go        # JSON helpers, header extraction
    ├── store/
    │   ├── store.go           # Persistence, tuple rehydration
    │   └── types.go           # Data structures
    └── templates/
        ├── home.html          # Main dashboard
        └── dossiers.html      # Dossier management UI
```

### Module Dependencies

```
main.go
├── internal/config      # ExternalURL, OpenfgaURL, AuditURL
├── internal/store       # DataStore, Load/Save, RehydrateTuples
├── internal/fga         # LoadConfig, Write, Check, ListObjects
├── internal/handlers    # HTTP handlers
└── internal/templates   # HTML templates (embed.FS)

handlers/*
├── internal/store       # Data access
├── internal/fga         # Authorization checks
├── internal/httputil    # Response helpers
├── internal/config      # URLs
└── internal/audit       # Audit logging
```

### Routes (main.go)

| Method | Path | Handler |
|--------|------|---------|
| GET | `/public` | inline |
| GET | `/api/protected` | inline |
| GET | `/api/health` | inline |
| GET | `/dossiers` | template render |
| GET | `/logout` | redirect |
| GET | `/api/dossiers/list` | DossiersList |
| GET | `/api/dossiers/admin/list` | DossiersListAll |
| POST | `/api/dossiers/create` | DossiersCreate |
| PUT | `/api/dossiers/{id}` | DossiersUpdate |
| DELETE | `/api/dossiers/{id}` | DossiersDelete |
| GET | `/api/dossiers/{id}/relations` | DossiersRelationsGet |
| POST | `/api/dossiers/{id}/relations` | DossiersRelationsAdd |
| DELETE | `/api/dossiers/{id}/relations` | DossiersRelationsDelete |
| POST | `/api/dossiers/{id}/toggle-public` | DossiersTogglePublic |
| POST | `/api/dossiers/{id}/block` | DossiersBlock |
| POST | `/api/dossiers/{id}/unblock` | DossiersUnblock |
| POST | `/api/dossiers/{id}/emergency-check` | DossiersEmergencyCheck |
| GET | `/api/dossiers/guardianships` | GuardianshipsList |
| POST | `/api/dossiers/guardianships/request` | GuardianshipRequest |
| POST | `/api/dossiers/guardianships/{id}/accept` | GuardianshipAccept |
| POST | `/api/dossiers/guardianships/{id}/deny` | GuardianshipDeny |
| DELETE | `/api/dossiers/guardianships/{id}` | GuardianshipRemove |
| GET | `/api/dossiers/guardianships/all` | GuardianshipsListAll |
| GET | `/api/dossiers/users` | UsersList |
| GET | `/api/dossiers/organizations` | OrganizationsList |
| POST | `/api/dossiers/organizations` | OrganizationsCreate |
| POST | `/api/dossiers/organizations/{id}/members` | OrganizationsAddMember |
| DELETE | `/api/dossiers/organizations/{id}/members` | OrganizationsRemoveMember |
| POST | `/api/dossiers/organizations/{id}/admins` | OrganizationsAddAdmin |
| DELETE | `/api/dossiers/organizations/{id}/admins` | OrganizationsRemoveAdmin |
| DELETE | `/api/dossiers/organizations/{id}` | OrganizationsDelete |
| GET | `/api/dossiers/debug/tuples` | DebugTuples |

### Key Functions

**fga/client.go:**
- `LoadConfig()` → Poll `/shared/openfga-store.json` (30 retries)
- `Write(writes, deletes)` → Write/delete tuples
- `Check(user, relation, object)` → Permission check
- `CheckWithContext(user, relation, object, contextualTuples)` → Emergency access
- `ListObjects(user, relation, type)` → List accessible objects

**store/store.go:**
- `Load()` → Read from `/data/dossiers.json`
- `Save()` → Persist to disk
- `RehydrateTuples()` → Rebuild FGA state from persisted data

---

## ai-manager (Node.js)

### Directory Structure

```
ai-manager/
├── server.js              # Express server, all routes
├── package.json           # Dependencies
├── Dockerfile             # Node.js 18 alpine
└── public/
    ├── index.html         # SPA shell
    └── app.js             # Frontend logic (tabs, API calls)
```

### Dependencies

```json
{
  "express": "^4.18.2",
  "express-rate-limit": "^7.1.5",
  "express-session": "^1.17.3",
  "openid-client": "^5.6.4",
  "axios": "^1.6.0",
  "@google/generative-ai": "^0.21.0",
  "zod": "^3.22.0"
}
```

### Routes (server.js)

**Authentication:**
| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/auth/status` | No | Check session |
| GET | `/auth/login` | No | Start OIDC |
| GET | `/auth/callback` | No | OIDC callback |
| GET | `/auth/logout` | No | Logout |

**Unauthenticated:**
| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/explain-authz` | AI explains 403 (from OPA page) |
| POST | `/logs` | OPA decision logs |
| POST | `/audit` | Audit entries from test-app |

**Authenticated (requireLogin):**
| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/audit` | Get audit logs |
| POST | `/api/generate-rule` | AI generates OPA rules |
| POST | `/api/apply-policy` | Apply rules to policy.rego |
| GET | `/api/rules/opa` | Read OPA policy |
| GET | `/api/rules/openfga` | Read FGA model |
| POST | `/api/chat` | Chat with AI |
| GET | `/api/visualize/opa` | Parse OPA for visualization |

**OpenFGA Management:**
| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/openfga/status` | Store/model status |
| GET | `/api/openfga/tuples` | List tuples |
| POST | `/api/openfga/tuples` | Add tuple |
| DELETE | `/api/openfga/tuples` | Delete tuple |
| GET | `/api/openfga/model` | Get auth model |
| GET | `/api/openfga/check` | Check permission |

**Proxy to test-app (Admin):**
| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/dossiers` | List all dossiers |
| PUT | `/api/dossiers/:id` | Update dossier |
| DELETE | `/api/dossiers/:id` | Delete dossier |
| POST | `/api/dossiers/:id/toggle-public` | Toggle public |
| POST | `/api/dossiers/:id/block` | Block user |
| POST | `/api/dossiers/:id/unblock` | Unblock user |
| * | `/api/dossiers/:id/relations` | Manage relations |
| GET | `/api/organizations` | List organizations |
| POST | `/api/organizations/:id/admins` | Add admin |
| DELETE | `/api/organizations/:id/admins` | Remove admin |
| DELETE | `/api/organizations/:id` | Delete org |
| GET | `/api/users` | List users |
| GET | `/api/guardianships` | List guardianships |

### Middleware

```
requireLogin     → Check session.userinfo exists
requireAdminRole → Check ai-admin role in session
```

### Rate Limiting

```
AI endpoints:     10 req / 15 min (generate-rule, chat, explain-authz)
Org endpoints:    30 req / 1 min
Dossier endpoints: 30 req / 1 min
```

### AI Integration

```
Google Gemini 2.0 Flash
├── generateRule()  → Generate OPA rules from natural language
├── chatWithAI()    → Conversational policy assistant
└── explainAuthz()  → Explain 403 denials
```

### Frontend (public/app.js)

**Tabs:**
1. Generate Rule - AI rule generation
2. Chat - Policy assistant
3. OpenFGA Manager - Tuple/model management
4. Dossiers - Admin dossier management
5. Organizations - Org management
6. Users & Guardianships - View relationships
7. Visualize - Mermaid diagrams
8. Audit Logs - Real-time audit viewer
