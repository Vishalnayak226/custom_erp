# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# DORMANT — not on the current deployment path.
#
# dev/test/live all run the plain erp-server binary under systemd/manage.ps1
# today (see docs/operations/go_live_decisions.md §4). This file exists only
# so containerizing later is a same-day switch instead of a from-scratch
# build, if/when real multi-instance scale ever justifies it (§4a.1's own
# estimate: not before a few hundred tenants on one well-sized box, or 5+ app
# instances — watch the Stage 26.1.2/26.1.5 System Status / Tenant Usage
# dashboards for the real signal, not a client-count guess).
#
# CI builds this image on every push (docker-build-check job in
# .github/workflows/ci.yml) so it can't silently rot before the day it's
# actually needed. CI never pushes or deploys it.
#
# Migrations are still applied the same way as today (psql against the
# target Postgres, same file list `manage.ps1`/CI already use) — independent
# of whether the app itself runs in a container.
# ---------------------------------------------------------------------------

FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/erp-server ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /out/erp-server ./erp-server
COPY public ./public

# DATABASE_URL, OPS_ALERT_WEBHOOK_URL, ENV=production, and any connector/GSP/
# Pine Labs credentials (docs/operations/go_live_decisions.md) are supplied at
# `docker run -e` / compose / orchestrator-secret time — never baked into the
# image.
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["./erp-server"]
