# Multi-Tenant SaaS ERP: Architectural & Technology Evaluation

This document outlines the recommended technology stack, database design, and release strategy for building a future-proof, highly scalable, and customizable multi-tenant ERP system.

---

## 1. Summary of Architectural Goals

1.  **Massive Scale & Low Server Cost**: Ability to spin up and manage thousands of clients (tenants) with minimal operational overhead and ultra-low server costs.
2.  **Ultra-Lightweight Footprint**: Avoid large dependencies (such as heavy Python virtual environments, JVM runtimes, or massive node_modules folders) that inflate cloud storage and RAM costs.
3.  **Customization Isolation**: Individual client customizations (custom fields, business rules, custom reports) must never affect other clients or break the core platform.
4.  **Hassle-Free Release Management**: A unified core codebase that supports rolling out global updates, while allowing client-specific feature flag controls.
5.  **Ultra-Solid & Observable**: Code compiles to a safe, crash-proof binary. When issues occur, they must be caught by global handlers and tracked in a centralized log console.

---

## 2. Recommended Technology Stack (Ultra-Lightweight Comparison)

To meet the requirement of keeping cloud server costs minimal and dependencies lightweight, we compare the standard runtime footprints below:

| Metric | Go (Recommended) | Python (FastAPI/Django) | Node.js (NestJS) |
| :--- | :--- | :--- | :--- |
| **Deployment Footprint** | **~15MB - 30MB** (Single binary) | **~300MB - 800MB** (Runtime + virtualenv) | **~250MB - 600MB** (Runtime + node_modules) |
| **Idle Memory Usage** | **~10MB - 15MB RAM** | **~80MB - 150MB RAM** | **~70MB - 120MB RAM** |
| **Startup Time** | **< 10 milliseconds** | **~1 - 3 seconds** | **~1 - 2 seconds** |
| **External Dependencies** | **None** (Self-contained binary) | **Requires python interpreter** + libs | **Requires Node interpreter** + packages |

### Why Go is the Cost-Saver:
- **Low RAM Overhead**: You can host **50 to 80 separate client microservices** on a single $5/month virtual server using Go, whereas Python or Node.js would consume all server memory with just 4 to 6 idle instances.
- **Ultra-Solid Execution**: Compile-time typing catches bugs before deployment. Go has no null-pointer exceptions or runtime interpreter crashes.

---

## 3. Database Multi-Tenancy Strategy

To support thousands of clients while maintaining schema flexibility and data isolation, we recommend a **Hybrid Schema-per-Tenant** architecture in PostgreSQL.

```
                  +-----------------------------------+
                  |            API Gateway            |
                  +-----------------------------------+
                                    | (Inspects JWT / Domain)
                                    v
                  +-----------------------------------+
                  |         Core ERP Services         |
                  +-----------------------------------+
                                    |
            +-----------------------+-----------------------+
            | (Reads Tenant Schema mapping)                 |
            v                                               v
+-----------------------+                       +-----------------------+
|  Tenant A (Schema A)  |                       |  Tenant B (Schema B)  |
|                       |                       |                       |
|  - core_tables        |                       |  - core_tables        |
|  - custom_attributes  |                       |  - custom_attributes  |
+-----------------------+                       +-----------------------+
```

### Why Schema-per-Tenant?
- **Data Isolation**: Database connection pools target specific client schemas. Client A can never accidentally query or overwrite Client B's data.
- **Custom Schemas**: You can apply custom tables and indexes to Client A's schema without affecting Client B.
- **Hassle-Free Migrations**: Database migration scripts are run programmatically per schema. If Client A has a custom branch, they can run a localized migration.

---

## 4. Customization Isolation Patterns

To ensure customizations do not impact the core code, we use **Metadata-Driven Customization** combined with **Serverless Extension Hooks**.

### 4.1 Custom Fields (Metadata-Driven)
Do not modify database tables directly when a client requests a new field. Instead, use a JSONB metadata mapping in core tables:
```sql
CREATE TABLE product_combination (
    id UUID PRIMARY KEY,
    mrp DECIMAL(10,2),
    -- Core fields...
    custom_attributes JSONB DEFAULT '{}'
);
```
The **Dynamic Label Engine** uses these metadata records to automatically render custom input fields on screen.

### 4.2 Custom Logics (Serverless Hooks)
If Client A requires a completely custom calculation logic (e.g., a complex custom jewelry pricing algorithm):
1.  Define a **Webhook Hook** in the core Go server.
2.  Route the custom logic to a tenant-specific Serverless Function (e.g., AWS Lambda, Cloudflare Worker, or Vercel Serverless).
3.  The core Go code executes standard validation, queries the serverless function, and updates the ledger.
4.  **Result**: Custom business code is isolated outside the core binary, preventing crashes for other tenants.

---

## 5. Release & Deployment Strategy

To achieve "hassle-free" universal release control, implement the following pipeline:

```
[Git Core Repository] ---> [CI/CD Build Go Binary] ---> [Container Registry (Docker)]
                                                                |
                                                                v
                                                +-------------------------------+
                                                |   Kubernetes / AWS ECS Cluster|
                                                |                               |
                                                |   - Core Service pods         |
                                                |   - Feature Flag Engine       |
                                                +-------------------------------+
```

### 5.1 Universal Codebase, Dynamic Releases
- **One Active Version**: Deploy a single, unified Docker container running the core Go backend. Do not maintain separate backend servers for separate clients.
- **Feature Flags (e.g. Flagsmith / LaunchDarkly)**: Enforce all client-specific release permissions via database-backed feature flags.
  - *Universal Release*: Toggle flag `ON` for everyone.
  - *Canary / Client Release*: Toggle flag `ON` for specific Client IDs (e.g. pilot client testing).
  - *Rollback*: Instantly toggle the flag `OFF` system-wide if a bug is found, without redeploying code.

---

## 6. Central Log Hub & Observability (Track & Fix Easily)

To ensure that any runtime error, integration failure, or user exception is instantly visible and easily patchable, a centralized **System Log & Exception Dashboard** is integrated into the core ERP admin panel.

### 6.1 Logging Protocol
1.  **Global Panic Recovery**: If a runtime crash occurs, a global middleware interceptor catches it, logs the stack trace to the DB, and gracefully returns a standard HTTP 500 error code, preventing the server instance from stopping.
2.  **Structured JSON Logs**: Every log entry writes structured metadata:
    - `correlation_id`: A unique UUID injected at the API gateway tracking a user's action path.
    - `tenant_id`: Maps the error to the specific client.
    - `error_context`: Details the database query, API URL, or payload that triggered the issue.

### 6.2 The Log Hub Console
Inside the ERP developer admin panel, a **System Log Hub** lists active logs:
- **Filters**: Tenant, Severity (Info/Warn/Error/Panic), Module, Date range, and correlation ID.
- **Error Details**: Shows formatted error messages, payload inputs, and file line locations (e.g. `db.go:L142`).
- **Action Hooks**: Includes a `Retry` trigger button for failed asynchronous tasks (such as Shopify updates or Pine Labs callback payments).
