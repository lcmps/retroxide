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

Uploads are limited to 64 MB by default. Set `RETROXIDE_MAX_UPLOAD_SIZE` in megabytes to change it, for example `RETROXIDE_MAX_UPLOAD_SIZE=4096MB` for 4 GB ROMs (a bare number also means megabytes, and `GB` works too). The older `RETROXIDE_MAX_UPLOAD_BYTES` is still read when the new setting is absent.

Uploads are identified by `RETROXIDE_WORKER_COUNT` background workers (default 1), and each new ROM is then scraped for metadata and artwork by `RETROXIDE_SCRAPE_WORKER_COUNT` separate workers (default 2), so slow metadata services never delay identification.

The **Profile & settings** page (the user icon at the top right) lets everyone change their email and password and read the settings that affect them. Administrators also see the server and storage settings and can edit the upload, session, network, and worker settings there. Edits are saved to `RETROXIDE_CONFIG_FILE` and take effect after a restart, except the interface font (`RETROXIDE_UI_FONT`: `pixel`, the default Press Start 2P; `ubuntu`; or `system`, the operating system's font), which applies immediately as the default for every visitor, including the sign-in page. Each user can also pick their own font and interface language on the same page; both are saved to their account, the font overriding the application default and the language overriding the browser's. Settings set through process environment variables override the file and stay read-only on the page.

Users sign in with their email or, if they set one, a username (3 to 32 lowercase letters, digits, dots, dashes, or underscores; matched case-insensitively). Administrators manage accounts on the **Users** page: they can create users, change emails, usernames, and roles, reset passwords (which signs that user out everywhere), sign a user out of every device, and see each user's ROM count, storage used, uploads, active sessions, and platforms. Deleting a user moves their ROMs, libraries, and uploads to the administrator who deleted them, so nothing is lost; administrators cannot delete or demote themselves, so at least one administrator always remains.

The Library page can be sorted by title, release date, publisher, or genre with the buttons beside its title: each click cycles that order through ascending, descending, and back to the default (the order ROMs were added). ROMs without a value for the chosen field are listed last either way, and the order is kept in the page URL with the search and platform filter. Genres are scraped from libretro's genre databases (not every system has one) and can be edited on the ROM page. The API takes the same order as `sort` (`title`, `release`, `publisher`, or `genre`) and `order` (`asc` or `desc`) on `GET /api/v1/rom`; each order is paged through indexes, so large libraries sort as quickly as they list.

Library search uses a full-text index. On SQLite it matches any part of a title or platform; on MySQL it matches words by their beginning (`mario k` finds "Super Mario Kart", a fragment from inside a word does not), which keeps searches fast on large libraries. MySQL connections send queries with client-side parameter interpolation (one round trip per query instead of three); add `interpolateParams=false` to `RETROXIDE_MYSQL_URL` to turn it off.

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