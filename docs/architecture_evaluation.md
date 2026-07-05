# Multi-Tenant SaaS ERP: Architectural & Technology Evaluation

This document outlines the recommended technology stack, database design, and release strategy for building a future-proof, highly scalable, and customizable multi-tenant ERP system.

---

## 1. Summary of Architectural Goals

1.  **Massive Scale**: Ability to spin up and manage thousands of clients (tenants) with minimal operational overhead.
2.  **Customization Isolation**: Individual client customizations (custom fields, business rules, custom reports) must never affect other clients or break the core platform.
3.  **Hassle-Free Release Management**: A unified core codebase that supports rolling out global updates, while allowing client-specific feature flag controls.
4.  **Future-Proof & Robust**: Zero-downtime deployments, high performance, and protection against system-wide failure cascades.

---

## 2. Recommended Technology Stack

| Layer | Recommended Technology | Rationale |
| :--- | :--- | :--- |
| **Backend Language** | **Go (Golang)** | Compiled single-binary, extremely fast, very low memory footprint (allows running thousands of microservices cheaply), and a strict backward-compatibility promise. |
| **Backend Framework** | **Standard Go Library + Gin/Fiber** | Keeps dependencies low to ensure the system never breaks due to third-party deprecations. |
| **Frontend Framework** | **TypeScript + React or Vue** | Type-safety reduces runtime crashes. Single Page Application (SPA) architecture allows caching assets on CDNs, reducing server load. |
| **Primary Database** | **PostgreSQL (Serverless)** | Industry-standard SQL database supporting robust transaction controls. Ideal for schema-per-tenant multi-tenancy. |
| **Database Hosting** | **Neon / AWS Aurora Serverless v2** | Automatically scales computed power to zero when a client is inactive, enabling thousands of clients to be hosted cost-effectively. |

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
