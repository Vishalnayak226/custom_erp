# In-House Enterprise ERP System

A metadata-driven, pluggable, and ledger-backed Enterprise Resource Planning (ERP) system designed to serve retail checkout, warehouses, e-commerce, and compliance matching across any industry.

## Project Structure

This repository is organized to support multi-AI development and version control:

```
├── .gitignore                  # Standard Git exclusions (node_modules, IDEs, system logs)
├── README.md                   # Project home and entry guide
├── docs/
│   ├── implementation_plan.md     # Unified Technical Specification Document (Logic & constraints)
│   ├── framework_architecture.md  # Metadata-driven pluggable DocType Kernel specification
│   ├── pos_architecture.md        # Pluggable offline POS terminal system specifications
│   ├── modules_overview.md        # Functional Modules Directory (app-plugin index)
│   ├── micro_checklist.md         # Micro-checklists and Stage 1-10 Build Tracker
│   └── architecture_evaluation.md # SaaS Multi-Tenant Cloud Scaling & Go runtime evaluation
├── index.html                  # Main Single Page Application UI view shell
├── app.js                      # Application state engine and UI render routers
├── db.js                       # In-memory mock database & INITIAL_ERP_DATA schemas
└── package.json                # App manifest and startup scripts
```

## Getting Started

### Running Locally
To launch the application server locally:
```bash
npm install
npm start
```
By default, this will serve the application on `http://localhost:8080`.

## Technical Reference & Architecture
For the complete technical breakdown, please refer to the files in the `docs/` folder:

*   **System Customizations**: Read **[docs/framework_architecture.md](docs/framework_architecture.md)** to understand how the dynamic DocType metadata schemas and UI form interpreters are structured.
*   **Checkout Logic**: Read **[docs/pos_architecture.md](docs/pos_architecture.md)** for details on the offline-first IndexedDB cache and cash session workflows.
*   **Database & Accounting**: Read **[docs/implementation_plan.md](docs/implementation_plan.md)** for double-entry GL mappings, validation matrices, and API specifications.
*   **Task Tracking**: Use **[docs/micro_checklist.md](docs/micro_checklist.md)** to mark, revise, and verify implemented stages.
