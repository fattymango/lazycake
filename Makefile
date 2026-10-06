MODULE := github.com/mkassab215/lazycake
BIN    := bin
GO     ?= go

.PHONY: build test lint migrate migrate-down proto clean web-check web-embed

build:
	mkdir -p $(BIN)
	$(GO) build -o $(BIN)/coordinator ./cmd/coordinator
	$(GO) build -o $(BIN)/agent ./cmd/agent
	$(GO) build -o $(BIN)/gateway ./cmd/gateway
	$(GO) build -o $(BIN)/lcctl ./cmd/lcctl
	$(GO) build -o $(BIN)/lcinit ./cmd/lcinit
	$(GO) build -o $(BIN)/refworkload ./cmd/refworkload

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

# Frontend (web/): type-check, lint, format check and unit tests.
web-check:
	cd web && npm run build >/dev/null && npx eslint src --ext ts,tsx && npm run format:check && npm test

# Build the frontend into the directory the coordinator embeds, then build the
# coordinator. (The Dockerfile does the same in its own build stage.)
web-embed:
	cd web && npx vite build --outDir ../internal/coordinator/webassets/dist --emptyOutDir
	git checkout -- internal/coordinator/webassets/dist/.gitkeep
	$(GO) build -o $(BIN)/coordinator ./cmd/coordinator
