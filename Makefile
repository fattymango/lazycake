MODULE := github.com/mkassab215/lazycake
BIN    := bin
GO     ?= go

.PHONY: build test lint migrate migrate-down proto clean

build:
	mkdir -p $(BIN)
	$(GO) build -o $(BIN)/coordinator ./cmd/coordinator
	$(GO) build -o $(BIN)/agent ./cmd/agent
	$(GO) build -o $(BIN)/gateway ./cmd/gateway
	$(GO) build -o $(BIN)/lcctl ./cmd/lcctl
	$(GO) build -o $(BIN)/lcinit ./cmd/lcinit

test:
	$(GO) test -race ./...

test-integration:
	$(GO) test -race -tags=integration ./...

lint:
	$(GO) vet ./...
	gofmt -l . | (! grep .)

migrate:
	goose -dir migrations postgres "$${LAZYCAKE_DATABASE_URL:-postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable}" up

migrate-down:
	goose -dir migrations postgres "$${LAZYCAKE_DATABASE_URL:-postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable}" down

proto:
	buf generate

clean:
	rm -rf $(BIN)
