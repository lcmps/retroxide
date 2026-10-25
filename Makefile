GO ?= go
BINARY ?= retroxide

.PHONY: build run test race vet check integration integration-mysql bench clean docker-build

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
