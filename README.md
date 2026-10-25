# Retroxide

Retroxide is a Go backend and SvelteKit frontend maintained in one repository. The applications keep separate dependency graphs and build artifacts.

## Layout

- `main.go`: Go backend entry point
- `internal/features/`: auth, ROM, upload, library, and job behavior
- `internal/adapters/`: Gin, GORM, hashing, auth, storage, and observability integrations
- `internal/bootstrap/`, `internal/platform/`, `internal/shared/`: wiring, config/IDs, and shared identity/errors
- `frontend/`: SvelteKit 2, Svelte 5, Vite, TypeScript, and Vitest frontend

## Development

Requirements: Go 1.27+, Node.js/npm compatible with `frontend/package-lock.json`, and a C compiler for SQLite.

The backend requires `RETROXIDE_JWT_SECRET` with at least 32 characters. The development `.env` placeholder is not accepted in production. Before starting Compose, set a real random value, for example:

```sh
export RETROXIDE_JWT_SECRET="$(openssl rand -base64 48)"
```

Run the backend from the repository root:

```sh
make run
```

In a second terminal, run the frontend dev server:

```sh
make frontend-install
make frontend-dev
```

Vite proxies `/api` and `/health` to `http://127.0.0.1:3000` by default. Set `VITE_API_PROXY_TARGET` to change the backend target.

Run checks independently or together:

```sh
make check       # Go tests, race detector, vet
make frontend-test frontend-check frontend-build
make check-all
```

The backend Docker image remains a backend-only artifact. Frontend production deployment is a separate SvelteKit build; configure a reverse proxy for same-origin API calls or set `VITE_API_BASE_URL` to the API origin.