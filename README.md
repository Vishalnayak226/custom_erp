# In-House Enterprise ERP System

A configurable, ledger-backed, audit-ready Enterprise Resource Planning (ERP) system designed for retail stores, warehouses, e-commerce channels, and compliance matching.

## Project Structure

This repository is organized to support multi-AI development and version control:

```
├── .gitignore               # Standard Git exclusions (node_modules, IDEs, system logs)
├── README.md                # Project home and entry guide
├── docs/
│   └── implementation_plan.md  # Unified Technical Specification Document (Logic & schemas)
├── index.html               # Main Single Page Application UI view shell
├── app.js                   # Application state engine and UI render routers
├── db.js                    # In-memory mock database & INITIAL_ERP_DATA schemas
└── package.json             # App manifest and startup scripts
```

## Getting Started

### Prerequisites
- Node.js (recommended for local HTTP serving)

### Running Locally
To launch the application server locally:
```bash
npm install
npm start
```
By default, this will serve the application on `http://localhost:8080`.

## Technical Reference & Architecture
For the complete technical breakdown covering **Central Engines, Database Schemas, Validation Constraints, Double-Entry GL Bookings, and API specs**, please refer to the master technical blueprint:

👉 **[docs/implementation_plan.md](docs/implementation_plan.md)**
