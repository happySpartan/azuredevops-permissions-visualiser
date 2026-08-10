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
WORKDIR /src
# Copy go.mod first for layer caching.
COPY backend/go.mod backend/go.sum* ./
COPY backend/ .
# Copy the built frontend (backend/web/dist is produced by Stage 1 under /src/web).
WORKDIR /src/backend
# Frontend assets are produced in the web stage; copy from that stage here.
COPY --from=web /src/backend/web/dist ./web/dist
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/visualiser ./cmd/server/

# ---- Stage 3: distroless runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/visualiser /visualiser
EXPOSE 8080
ENV PORT=8080
# Single-administrator local use: bind to localhost by default.
ENV BIND_ADDR=127.0.0.1
USER nonroot
ENTRYPOINT ["/visualiser"]