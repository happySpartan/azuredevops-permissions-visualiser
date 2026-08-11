.PHONY: web backend build release run dev clean docker

# Build the React/TS frontend into backend/web/dist
web:
	cd web && npm install && npm run build

# Build the Go backend binary (embeds backend/web/dist)
backend:
	cd backend && go build -o ../bin/visualiser ./cmd/server/

# Build both: frontend then backend
build: web backend

# Build the supported native release binaries after producing embedded assets.
release: web
	mkdir -p dist
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o ../dist/visualiser-linux-amd64 ./cmd/server/
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o ../dist/visualiser-windows-amd64.exe ./cmd/server/

# Run the backend (serves the built frontend). Requires backend/web/dist to exist.
run:
	cd backend && go run ./cmd/server/

# Dev: run the frontend dev server with API proxy in one terminal,
# and `make run` in another for the backend.
dev:
	cd web && npm run dev

.PHONY: clean
clean:
	rm -rf bin
	rm -rf dist
	rm -rf backend/web/dist
	rm -rf web/node_modules web/dist

# Build the Docker image (multi-stage: web + backend + distroless)
docker:
	docker build -t azuredevops-permissions-visualiser:latest .