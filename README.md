# Retroxide

Retroxide is a Go backend and SvelteKit frontend maintained in one repository and shipped as a single self-contained binary: the frontend is built as static files and embedded into the Go executable, which serves the web interface and the API on one port.

## Layout

- `main.go`: Go backend entry point
- `domain/`: entities, value objects, identity, and invariant checks
- `application/`: use cases, DTOs, dependency interfaces, workers, and application errors
- `infrastructure/`: GORM repositories, filesystem storage, hashing, tokens, configuration, scraping, and audit logging
- `interface/http/`: Gin routing, middleware, presenters, and embedded locale catalogs
- `interface/web/`: embedded frontend build (`dist/`, generated) and its static file server
- `factory/`: explicit factories for configuration, repositories, storage, services, routing, and application assembly
- `bootstrap/`: HTTP/worker lifecycle, signals, and shutdown
- `frontend/`: SvelteKit 3, Svelte 5, Vite, TypeScript, and Vitest frontend

## Backend Architecture

Domain packages depend only on domain packages and the standard library.
Application use cases consume domain types and define the interfaces implemented
by infrastructure. HTTP adapts requests to application use cases. Concrete
dependencies are constructed only at the composition boundary in `factory/`,
using ordinary typed constructors without a DI framework or global registry.

`bootstrap.Build` delegates to `factory.NewApplication`, which opens one database,
shares repositories between HTTP services and workers, seeds the configured admin,
and builds the router. Failed construction closes the database; after successful
construction, bootstrap owns runtime startup and shutdown. Tests in `factory/`
enforce dependency direction and cover assembly and failure cleanup.

## Building and Running

Requirements: Go 1.27+ and Node.js/npm compatible with `frontend/package-lock.json`. No C compiler is needed: SQLite uses a pure-Go driver and all builds run with `CGO_ENABLED=0`.

```sh
make build          # installs frontend deps if needed, builds the frontend, then bin/retroxide
./bin/retroxide     # serves the web interface and API on http://localhost:3000
make release        # cross-compiles bin/retroxide-<os>-<arch> for Linux, macOS, and Windows
```

The binary needs no other files. On first run it opens the setup wizard; the database, ROM storage, and config file it creates live outside the binary. Paths outside `/api` serve the web app (client routes fall back to `index.html`); fingerprinted assets are cached for a year, `index.html` is revalidated on every load, and pre-compressed Brotli/gzip files are served when the browser accepts them. Building the backend without the frontend (`make backend-build`, `go build`) still works and serves a notice in place of the web interface.

The binary serves plain HTTP. To expose it beyond a trusted network, put it behind a TLS-terminating reverse proxy such as Caddy or nginx.

`docker compose up` builds the same single binary in a multi-stage image (Node for the frontend, then Go) and serves everything on port 3000.

## Development

For live reload, run the backend and the Vite dev server side by side.

### First-run setup

If `RETROXIDE_JWT_SECRET` is not set in the environment or config file, the backend starts in setup mode and the frontend redirects to `/setup`. The wizard configures the database (SQLite, or MySQL with a connection test), the ROM storage folder, and the administrator account. It generates a JWT secret, verifies the database, writes the configuration to `RETROXIDE_CONFIG_FILE` (default `./.env`, mode `0600`), and switches to the full application without a restart. Admin credentials are used once for seeding and are not saved. Fields already set through environment variables are shown read-only.

Setup endpoints require no authentication and are available only until the configuration is saved; complete setup before exposing the server to untrusted networks.

The backend requires `RETROXIDE_JWT_SECRET` with at least 32 characters, which the setup wizard generates. The development `.env` placeholder is not accepted in production. To provide your own instead, set a random value, for example:

```sh
export RETROXIDE_JWT_SECRET="$(openssl rand -base64 48)"
```

Access tokens last one hour (`RETROXIDE_JWT_ACCESS_EXPIRY_SECS`) and refresh tokens seven days (`RETROXIDE_JWT_REFRESH_EXPIRY_SECS`). Every request re-checks the token against the user record, so logging out ends all of that user's sessions immediately and role changes apply on the next request.

Login and token-refresh requests are throttled per client IP. By default the backend ignores `X-Forwarded-For`/`X-Real-IP` and uses the TCP peer address. Behind a reverse proxy, set `RETROXIDE_TRUSTED_PROXIES` to the proxy's addresses (comma-separated IPs or CIDR ranges, for example `127.0.0.1,10.0.0.0/8`) so throttling applies to real clients instead of the proxy.

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
make check       # Go tests, race detector (requires a C compiler), vet
make frontend-test frontend-check frontend-build
make check-all
```