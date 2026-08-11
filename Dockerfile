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
FROM golang:1.25-alpine AS build
WORKDIR /src/backend
# Copy go.mod first for layer caching.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Frontend assets are produced in the web stage; copy from that stage here.
COPY --from=web /src/backend/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /out/visualiser ./cmd/server/

# ---- Stage 3: runtime with the required Azure CLI ----
FROM mcr.microsoft.com/azure-cli:2.89.0-azurelinux3.0
COPY --from=build /out/visualiser /visualiser
EXPOSE 8080
ENV PORT=8080
# Listen inside the container; publish it on the host loopback interface only.
ENV BIND_ADDR=0.0.0.0
ENV NO_BROWSER=1
ENTRYPOINT ["/visualiser"]