# ReBAC Scenarios — Citizen Mandate System

This document describes all 8 relationship-based access control (ReBAC) scenarios implemented in the Citizen Mandate System using OpenFGA.

---

## Overview

| # | Scenario | Pattern | Status |
|---|----------|---------|--------|
| 1 | Direct Ownership | Direct relation | Existing |
| 2 | Mandate Delegation | Direct relation | Existing |
| 3 | Guardian Traversal | `tupleToUserset` | Existing |
| 4 | Guardianship Workflow | Request/Accept/Deny/Remove | Existing |
| 5 | Organization Access | `tupleToUserset` on organization type | **New** |
| 6 | User Blocking | `difference` (exclusion) | **New** |
| 7 | Public Dossiers | Wildcard `user:*` | **New** |
| 8 | Emergency Access | Contextual tuples | **New** |

---

## Scenario 1: Direct Ownership

**Pattern:** Direct relation (`owner`)

The dossier owner automatically has `viewer` and `editor` access via computed usersets.

**Model excerpt:**
```
viewer = owner | mandate_holder | ...
editor = owner | mandate_holder | ...
```

**Tuple:**
```
user:alice  owner  dossier:d1
```

**API:** `POST /api/dossiers/create` — creates the dossier and writes the owner tuple.

**Tests:** `TestDossiersCreate_Valid`, `TestDossiersList_WithDossiers`

---

## Scenario 2: Mandate Delegation

**Pattern:** Direct relation (`mandate_holder`)

A dossier owner can grant a mandate to another user, giving them view+edit access.

**Tuple:**
```
user:bob  mandate_holder  dossier:d1
```

**API:** `POST /api/dossiers/{id}/relations` with `{ "targetUser": "bob" }`

**Constraint:** The target user must be in a guardianship relationship with the owner.

---

## Scenario 3: Guardian Traversal

**Pattern:** `tupleToUserset`

If Alice has a guardian (Bob), Bob can automatically view Alice's dossiers without an explicit mandate — the access is computed by traversing the `owner -> guardian` relationship.

**Model excerpt:**
```
can_view = ... | owner->guardian
```

**Tuples:**
```
user:bob   guardian  user:alice
user:alice owner    dossier:d1
```

Result: Bob can view `dossier:d1` because he is a guardian of the owner.

---

## Scenario 4: Guardianship Workflow

**Pattern:** Request/Accept/Deny/Remove workflow

Guardianship is established through a multi-step workflow:

1. Alice requests to guard Bob: `POST /api/dossiers/guardianships/request` with `{ "to": "bob" }`
2. Bob accepts: `POST /api/dossiers/guardianships/{reqId}/accept`
3. Guardian tuple is written: `user:alice guardian user:bob`
4. Either party can remove: `DELETE /api/dossiers/guardianships/{userId}`

**Tests:** `TestGuardianshipRequest_Valid`, `TestGuardianshipsList_WithData`

---

## Scenario 5: Organization Access (NEW)

**Pattern:** `tupleToUserset` on `organization` type

A government department (organization) can share dossiers with all its members. When a dossier is assigned to an organization via `org_parent`, all organization members gain viewer access.

Organizations have an **admin** role: the creator of an organization automatically becomes an admin. Only admins can add/remove members and promote/demote other admins. The `can_manage` computed relation resolves to `admin` and gates all management operations.

**Model excerpt:**
```
type organization
  relations
    member: [user]
    admin: [user]
    can_manage = admin

type dossier
  relations
    org_parent: [organization]
    can_view = ... | org_parent->member
```

**Tuples:**
```
user:alice     member      organization:bosa
user:alice     admin       organization:bosa
organization:bosa  org_parent  dossier:d1
```

Result: Alice can view `dossier:d1` because she is a member of organization BOSA, which is the org_parent of the dossier. Alice can also manage members and admins because she has the `admin` relation.

**API endpoints:**
- `GET /api/dossiers/organizations` — list all organizations (includes `admins` field)
- `POST /api/dossiers/organizations` — create organization with `{ "name": "BOSA", "members": ["alice"] }` (creator becomes admin automatically)
- `POST /api/dossiers/organizations/{id}/members` — add member with `{ "member": "bob" }` (admin only)
- `DELETE /api/dossiers/organizations/{id}/members` — remove member with `{ "member": "bob" }` (admin only)
- `POST /api/dossiers/organizations/{id}/admins` — promote member to admin with `{ "user": "bob" }` (admin only)
- `DELETE /api/dossiers/organizations/{id}/admins` — demote admin with `{ "user": "bob" }` (admin only, user remains member)
- `POST /api/dossiers/create` with `{ "orgId": "org1", ... }` — assign dossier to organization

**Tests:** `TestOrganizationsCreate`, `TestOrganizationsCreate_CreatorBecomesAdmin`, `TestOrganizationsAddMember_AsAdmin`, `TestOrganizationsAddMember_Unauthorized`, `TestOrganizationsRemoveMember_Unauthorized`, `TestOrganizationsAddAdmin`, `TestOrganizationsRemoveAdmin`, `TestDossierOrgAccess`, `TestDossiersCreate_WithOrgAndPublic`

**Demo walkthrough:**
1. Create organization "BOSA" with alice as member (alice becomes admin)
2. Create a dossier with `orgId` pointing to BOSA
3. Alice can see the dossier (org member); bob cannot
4. Alice (admin) adds bob to BOSA -> bob can now see the dossier
5. Bob (non-admin) tries to add charlie -> 403 Forbidden
6. Alice promotes bob to admin -> bob can now manage members
7. Alice demotes bob -> bob loses admin but remains a member

---

## Scenario 6: User Blocking (NEW)

**Pattern:** `difference` (exclusion / deny-override)

Even if a user has access through org membership, guardianship, or public access, the owner can block them. The `viewer` relation uses OpenFGA's `difference` operator: `can_view BUT NOT blocked`.

**Model excerpt:**
```
type dossier
  relations
    blocked: [user]
    can_view = owner | mandate_holder | owner->guardian | org_parent->member | public
    viewer = can_view but not blocked
```

**Tuples:**
```
user:bob  blocked  dossier:d1
```

Result: Bob cannot view `dossier:d1` even if he would otherwise have access through org membership or guardianship.

**API endpoints:**
- `POST /api/dossiers/{id}/block` with `{ "targetUser": "bob" }` — block a user
- `POST /api/dossiers/{id}/unblock` with `{ "targetUser": "bob" }` — unblock a user

**Tests:** `TestDossierBlockedUser`, `TestDossierBlockedUser_NotOwner`, `TestDossierUnblock`

**Demo walkthrough:**
1. Alice creates a dossier and adds bob to the org
2. Bob can view the dossier (org member)
3. Alice blocks bob on the dossier
4. Bob can no longer view it (blocked overrides org access)
5. Alice unblocks bob -> bob regains access

---

## Scenario 7: Public Dossiers (NEW)

**Pattern:** Wildcard `user:*`

A dossier can be marked as public, making it visible to all authenticated users. This uses OpenFGA's wildcard feature.

**Model excerpt:**
```
type dossier
  relations
    public: [user with wildcard]
    can_view = ... | public
```

**Tuple:**
```
user:*  public  dossier:d1
```

Result: Any authenticated user can view `dossier:d1`.

**API endpoints:**
- `POST /api/dossiers/{id}/toggle-public` — toggle public on/off (owner only)
- `POST /api/dossiers/create` with `{ "public": true, ... }` — create a public dossier

**Tests:** `TestDossierTogglePublic`, `TestDossierTogglePublic_NotOwner`, `TestPublicDossierVisibleToAll`

**Demo walkthrough:**
1. Alice creates a dossier
2. Random user cannot see it
3. Alice toggles public ON
4. Wildcard tuple `user:* public dossier:d1` is written
5. Any logged-in user can now see the dossier
6. Alice toggles public OFF -> access revoked for non-related users

---

## Scenario 8: Emergency Access (NEW)

**Pattern:** Contextual tuples (non-persisted, per-check)

Contextual tuples allow granting temporary access without persisting any tuples in the store. The tuple exists only for the duration of the check call. This is useful for emergency/break-glass scenarios.

**No model change needed** — contextual tuples are passed at check time via the OpenFGA API.

**API endpoint:**
- `POST /api/dossiers/{id}/emergency-check` with `{ "user": "bob", "relation": "viewer" }`

This sends a check request to OpenFGA with a contextual tuple:
```json
{
  "tuple_key": { "user": "user:bob", "relation": "viewer", "object": "dossier:d1" },
  "contextual_tuples": {
    "tuple_keys": [
      { "user": "user:bob", "relation": "can_view", "object": "dossier:d1" }
    ]
  }
}
```

Result: The check returns `allowed: true` for this single request. No tuple is persisted — the next check without context will return `denied`.

**Tests:** `TestEmergencyCheck`, `TestEmergencyCheck_NotFound`

**Demo walkthrough:**
1. Bob has no access to Alice's dossier
2. Admin calls emergency-check with bob as user -> access granted (contextual)
3. Normal check for bob -> still denied (no persisted tuple)

---

## Architecture

### OpenFGA Model

The full authorization model is defined in `infra/openfga/init.js` and includes three types:

- **user** — with `guardian` relation (for guardianship traversal)
- **organization** — with `member`, `admin`, and `can_manage` relations (for org-based access and admin management)
- **dossier** — with `owner`, `mandate_holder`, `org_parent`, `blocked`, `public`, `can_view`, `viewer`, `editor` relations

### Key Files

| File | Purpose |
|------|---------|
| `infra/openfga/init.js` | OpenFGA authorization model definition |
| `test-app/internal/store/types.go` | Data structures (Dossier, Organization, etc.) |
| `test-app/internal/store/store.go` | Persistence, tuple rehydration |
| `test-app/internal/fga/client.go` | OpenFGA API client (Check, CheckWithContext, Write, ListObjects) |
| `test-app/internal/handlers/dossiers.go` | Dossier CRUD + public/block/emergency handlers |
| `test-app/internal/handlers/organizations.go` | Organization CRUD handlers |
| `test-app/internal/handlers/guardianships.go` | Guardianship workflow handlers |
| `test-app/main.go` | HTTP routes |
| `test-app/internal/templates/dossiers.html` | UI with org/public/block/emergency sections |
