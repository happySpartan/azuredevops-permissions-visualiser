# syntax=docker/dockerfile:1

# ---- Stage 1: build the web frontend ----
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
# Build directly into ../backend/web/dist so the Go embed picks it up.
RUN npm run build

# ---- Stage 2: build the Go backend ----
FROM golang:1.26.6-alpine AS build
WORKDIR /src/backend
# Copy go.mod first for layer caching.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Frontend assets are produced in the web stage; copy from that stage here.
COPY --from=web /src/backend/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /out/visualiser ./cmd/server/

# ---- Stage 3: runtime with the required Azure CLI ----
# az is bundled into the image, so we stay on the Microsoft azure-cli base. We
# pin a newer 2.89.x tag (fresh OS package advisories) and then run a tdnf
# upgrade so libarchive/libssh2/python3 pull in the same-day security fixes
# (libarchive 3.7.7-7, libssh2 1.11.1-5, python3 3.12.9-14). The remaining trivy
# HIGH findings (bundled pip wheels: cryptography/msgpack/setuptools) are
# documented in .trivyignore.yaml -- they are frozen in the upstream image's
# venv and cannot be upgraded without breaking `az`.
FROM mcr.microsoft.com/azure-cli:2.89.1-azurelinux3.0
COPY --from=build /out/visualiser /visualiser
# The Azure Linux azure-cli image has no useradd (shadow-utils). Use a numeric
# non-root UID and pre-create + chown the runtime dirs instead, so the binary
# runs unprivileged and az can write its config under /home/visualiser/.azure.
RUN tdnf update -y --refresh \
    && mkdir -p /data /home/visualiser/.azure \
    && chown -R 10001:10001 /data /home/visualiser \
    && chmod 0700 /home/visualiser/.azure \
    && tdnf clean all
EXPOSE 8080
ENV PORT=8080
# Listen inside the container; publish it on the host loopback interface only.
ENV BIND_ADDR=0.0.0.0
ENV NO_BROWSER=1
ENV AZDO_VIS_DATA_DIR=/data
ENV AZURE_CONFIG_DIR=/home/visualiser/.azure
USER 10001
ENTRYPOINT ["/visualiser"]