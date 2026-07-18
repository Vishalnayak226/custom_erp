# Stage 14.25: single-stage-to-run containerization, deliberately deferred
# until now (see docs/micro_checklist.md's Stage 14 section) - nothing is
# deployed to any cloud yet, so this exists as ready-to-use tooling for
# whenever that becomes real, not as evidence a container is already
# running somewhere.
#
# Two build stages (builder + runtime), but the *runtime* image is a single
# stage with nothing else in it - a static Go binary needs no OS, no libc,
# no package manager. CGO_ENABLED=0 is safe here because this project's two
# dependencies (lib/pq, golang.org/x/crypto/bcrypt) are both pure Go.
#
# NOT verified with a real `docker build`/`docker run` in this session -
# Docker isn't installed on this dev machine. What *was* verified: a native
# `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` produces a working static
# ELF binary with the exact same flags this Dockerfile uses. Run the
# verification steps in the comment at the bottom of this file before
# trusting this in anything real.

FROM golang:1.22.12-alpine AS builder
WORKDIR /src

# Dependencies first, so `docker build` only re-downloads modules when
# go.mod/go.sum actually change, not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG GIT_COMMIT=docker
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /erp-server .

# ---- Runtime: scratch, nothing but the binary + what it reads off disk ----
FROM scratch

# ca-certificates for outbound HTTPS - extension hooks (engines/extensions.go)
# and the Shopify integration both make outbound HTTP(S) calls that need TLS
# verification, and scratch has no certificate store of its own.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=builder /erp-server /erp-server
# Static frontend assets - handleGenericDoc's sibling http.FileServer serves
# these directly off disk (main.go: http.Dir("./public")), so they have to
# actually be present next to the binary, not just embedded in it.
COPY public/ /public/
COPY VERSION /VERSION

# Media Library uploads (engines/pim_media.go, Stage 15.2) land under
# ./media_store/ relative to the working directory - ephemeral inside the
# container unless a volume is mounted here. Not created automatically by
# scratch's COPY (there's nothing to copy) - the app creates it on first
# upload, but mount a volume at /media_store for anything beyond a
# throwaway/test container.

WORKDIR /
EXPOSE 8080
ENTRYPOINT ["/erp-server"]

# --- How to actually verify this once Docker is available (not done here) ---
#   docker build -t custom-erp:local .
#   docker run --rm -p 8080:8080 \
#     -e DATABASE_URL="postgres://postgres@host.docker.internal:5435/custom_erp?sslmode=disable" \
#     -e PORT=8080 \
#     custom-erp:local
#   curl http://localhost:8080/api/v1/version
# Every runtime knob this binary already reads (PORT, DATABASE_URL,
# JWT_SECRET, CORS_ALLOWED_ORIGINS, JWT_EXPIRY_HOURS, SHOPIFY_WEBHOOK_SECRET)
# works identically here via `docker run -e` - none of this Dockerfile
# hardcodes any of them.
