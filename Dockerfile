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

# Runtime base image, global (declared before the first FROM) so it is in scope
# for the runtime stage's own FROM at the bottom. Overridable, so the
# shell-free image can be pinned back at any time without editing this file:
#   docker build --build-arg RUNTIME_BASE=gcr.io/distroless/static-debian12
#
# The default carries `:debug`, which is the same distroless image plus a
# busybox shell at /bin/sh and nothing else — same `nonroot` user, same
# static-only contents otherwise. It is there because a PaaS that applies
# migrations through a pre-deploy/release hook runs that command **through a
# shell**: Railway's `./erp-server -migrate` (Stage 56.1/56.5) fails on the
# shell-free image with `exec: "sh": executable file not found`, which reads
# like a missing binary and is not one. The migration runner itself is
# unaffected either way — it is `go:embed`ed in the binary (db/migrate.go), so
# no `db/` directory is needed in the image.
#
# REVISIT BEFORE ANY CUTOVER THAT MAKES A CONTAINER PATH PRODUCTION
# (Stage 56.12). A shell in the runtime image is a real widening of the Stage
# 49.S2 minimal-attack-surface posture, justified only while the migrate hook
# needs it. Two ways out, either of which should retire this default: run
# migrations as a one-off job that does not go through a shell, or confirm the
# platform can invoke the hook in exec form. Today's production (the droplet)
# runs the plain binary under systemd and is NOT affected by this line at all —
# only the container path is.
ARG RUNTIME_BASE=gcr.io/distroless/static-debian12:debug

FROM golang:1.27-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build identity, same ldflags shape every other build path uses
# (manage.ps1, deploy/deploy.ps1, promote.ps1, CI) so a container build shows
# the same version/release date in the UI as a native one. Both default to the
# unstamped values, so a bare `docker build` still works.
ARG GIT_COMMIT=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X custom_erp/internal/server.gitCommit=${GIT_COMMIT} -X custom_erp/internal/server.buildTime=${BUILD_TIME}" \
    -o /out/erp-server ./cmd/server

# Runtime stage. RUNTIME_BASE is declared global at the top of this file —
# it has to be, since an ARG used in a FROM must be declared before the first
# FROM to be in scope; declared inside the builder stage it would expand to
# empty here and the build would fail on a bare `FROM`.
FROM ${RUNTIME_BASE}
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
