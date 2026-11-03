GO ?= go
NPM ?= npm
BINARY ?= retroxide
VERSION ?= dev
GO_LDFLAGS := -s -w -X main.version=$(VERSION)
WEB_DIST := interface/web/dist
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

export CGO_ENABLED := 0

.PHONY: build backend-build release run test race vet check check-all integration integration-mysql bench clean docker-build frontend-install frontend-dev frontend-test frontend-check frontend-build

BENCH_COUNT ?= 5

# The binary embeds the built frontend, so it must be compiled afterwards even under `make -j`.
build: frontend-build
	$(MAKE) backend-build

backend-build:
	$(GO) build -trimpath -ldflags="$(GO_LDFLAGS)" -o bin/$(BINARY) .

release: frontend-build
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
		echo "building bin/$(BINARY)-$$os-$$arch$$ext"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags="$(GO_LDFLAGS)" -o bin/$(BINARY)-$$os-$$arch$$ext . || exit 1; \
	done

run:
	$(GO) run .

test:
	$(GO) test ./...

# The race detector requires cgo.
race:
	CGO_ENABLED=1 $(GO) test -race ./...

vet:
	$(GO) vet ./...

check: test race vet

check-all: check frontend-test frontend-check frontend-build

frontend/node_modules: frontend/package-lock.json
	cd frontend && $(NPM) ci
	@touch frontend/node_modules

frontend-install:
	cd frontend && $(NPM) ci

frontend-dev:
	cd frontend && $(NPM) run dev -- --host 0.0.0.0

frontend-test:
	cd frontend && $(NPM) test

frontend-check:
	cd frontend && $(NPM) run check

frontend-build: frontend/node_modules
	cd frontend && $(NPM) run build
	@touch $(WEB_DIST)/.gitkeep

integration:
	$(GO) test -tags=integration ./...

integration-mysql:
	test -n "$(RETROXIDE_TEST_MYSQL_URL)"
	RETROXIDE_TEST_MYSQL_URL="$(RETROXIDE_TEST_MYSQL_URL)" $(GO) test -tags=integration ./infrastructure/persistence

bench:
	$(GO) test -run='^$$' -bench=. -benchmem -count=$(BENCH_COUNT) ./infrastructure/hash ./infrastructure/persistence

docker-build:
	docker build -t retroxide:local .

clean:
	rm -rf bin
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
