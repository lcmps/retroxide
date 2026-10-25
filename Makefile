GO ?= go
NPM ?= npm
BINARY ?= retroxide

.PHONY: build run test race vet check check-all integration integration-mysql bench clean docker-build frontend-install frontend-dev frontend-test frontend-check frontend-build

BENCH_COUNT ?= 5

build:
	$(GO) build -trimpath -o bin/$(BINARY) .

run:
	$(GO) run .

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check: test race vet

check-all: check frontend-test frontend-check frontend-build

frontend-install:
	cd frontend && $(NPM) ci

frontend-dev:
	cd frontend && $(NPM) run dev -- --host 0.0.0.0

frontend-test:
	cd frontend && $(NPM) test

frontend-check:
	cd frontend && $(NPM) run check

frontend-build:
	cd frontend && $(NPM) run build

integration:
	$(GO) test -tags=integration ./...

integration-mysql:
	test -n "$(RETROXIDE_TEST_MYSQL_URL)"
	RETROXIDE_TEST_MYSQL_URL="$(RETROXIDE_TEST_MYSQL_URL)" $(GO) test -tags=integration ./internal/adapters/persistence

bench:
	$(GO) test -run='^$$' -bench=. -benchmem -count=$(BENCH_COUNT) ./internal/adapters/hash ./internal/adapters/persistence

docker-build:
	docker build -t retroxide-backend:local .

clean:
	rm -rf bin
