# Architecture Codemap

> **Freshness:** 2026-02-04 | **Version:** 1.0.0

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Externalized Authorization POC                   │
│                                                                         │
│  Pattern: Sidecar/Gateway with ABAC (OPA) + ReBAC (OpenFGA)            │
│  Core App: Citizen Mandate System (8 ReBAC scenarios)                   │
└─────────────────────────────────────────────────────────────────────────┘
```

## Request Flow

```
User Request
     │
     ▼
┌─────────────┐
│   Envoy     │ :8000 (Gateway)
│  Proxy      │
└─────┬───────┘
      │
      ├──► OAuth2 Filter ──► Keycloak (OIDC login if no JWT)
      │
      ▼
┌─────────────┐
│    OPA      │ :8181 (ext_authz gRPC)
│  (ABAC)     │ Verifies JWT, evaluates policy.rego
└─────┬───────┘
      │
      ├──► ALLOW ──► Backend (test-app or ai-manager)
      │
      └──► DENY ──► 403 HTML with "Explain with AI" button
```

## Service Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              INFRASTRUCTURE                               │
│                                                                          │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐          │
│  │ Postgres │◄───│ Keycloak │    │   OPA    │    │  OpenFGA │          │
│  │  :5432   │    │  :8080   │    │  :8181   │    │  :8081   │          │
│  └────┬─────┘    └──────────┘    └────┬─────┘    └────┬─────┘          │
│       │                               │               │                  │
│       └───────────────────────────────┴───────────────┘                  │
│                              │                                           │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │                        Envoy Proxy :8000                          │   │
│  │  Routes: /login→KC, /manager→AI, /grafana→Grafana, /*→test-app   │   │
│  └──────────────────────────────────────────────────────────────────┘   │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│                              APPLICATIONS                                 │
│                                                                          │
│  ┌────────────────────┐         ┌────────────────────┐                  │
│  │     test-app       │         │    ai-manager      │                  │
│  │     (Go :3000)     │◄───────►│   (Node.js :5000)  │                  │
│  │                    │  proxy  │                    │                  │
│  │  Citizen Mandate   │  calls  │  Policy Management │                  │
│  │  System (ReBAC)    │         │  + Gemini AI       │                  │
│  └────────────────────┘         └────────────────────┘                  │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│                            OBSERVABILITY                                  │
│                                                                          │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐                           │
│  │ Promtail │───►│   Loki   │◄───│ Grafana  │                           │
│  │ (logs)   │    │  (store) │    │  (UI)    │                           │
│  └──────────┘    └──────────┘    └──────────┘                           │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

## Service Inventory

| Service | Technology | Port | Role |
|---------|------------|------|------|
| envoy | Envoy v1.26 | 8000, 9901 | API Gateway, OAuth2, ext_authz |
| keycloak | Keycloak 22.0 | 8080 | Identity Provider (OIDC) |
| opa | OPA latest-envoy | 8181, 9191 | Policy Decision Point (ABAC) |
| openfga | OpenFGA latest | 8081, 8082, 3001 | Relationship-Based AC |
| postgres | PostgreSQL 15 | 5432 | Persistence (KC + FGA) |
| test-app | Go 1.21 | 3000 | Citizen Mandate System |
| ai-manager | Node.js 18 | 5000 | Policy Management UI |
| loki | Grafana Loki 3.0 | 3100 | Log Aggregation |
| promtail | Promtail 3.0 | - | Log Collector |
| grafana | Grafana 11.0 | 3000 | Dashboards |

## Authorization Layers

### Layer 1: ABAC (OPA)

```
infra/opa/policies/policy.rego
├── JWT verification (Keycloak JWKS)
├── Path-based rules (/public, /api/*, /dossiers)
├── Role extraction from token
└── Custom 403 page with AI explain
```

### Layer 2: ReBAC (OpenFGA)

```
infra/openfga/init.js
├── type: user (guardian relation)
├── type: organization (member, admin, can_manage)
└── type: dossier (owner, mandate_holder, blocked, public, viewer, editor)
```

## Key Files

| Path | Purpose |
|------|---------|
| `docker-compose.yml` | Service orchestration (13 services) |
| `infra/envoy/envoy.yaml` | Gateway routing + auth filters |
| `infra/opa/policies/policy.rego` | ABAC authorization rules |
| `infra/openfga/init.js` | ReBAC model definition |
| `infra/keycloak/realm.json` | IdP configuration |
| `test-app/main.go` | Backend routes |
| `ai-manager/server.js` | Management API |

## Network Topology

```
External: :8000 (Envoy)
          :8081 (OpenFGA HTTP)
          :8082 (OpenFGA gRPC)
          :3001 (OpenFGA Playground)
          :8181 (OPA API)
          :9901 (Envoy Admin)

Internal: auth-net (bridge)
          All services communicate via container names
```
