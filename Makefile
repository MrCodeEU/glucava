.PHONY: help hooks build test vet fmt lint vuln check mock demo dev run cli token dexcom strava-check strava-photo strava-hr strava-list strava-cookies shot mailshot site reset-mock reset-real clean

SHELL := /bin/bash
BIN := glucava
PORT ?= 8090
ADDR ?= 127.0.0.1:$(PORT)
DEMO_PW ?= demo-password-123

# Real-mode settings (GLUCAVA_ADMIN_EMAIL, GLUCAVA_ADMIN_PASSWORD, CHROME_PATH,
# GLUCAVA_TRUSTED_PROXIES, ...) come from .env; copy .env.example. Plain KEY=value lines only.
-include .env
export

# Chrome for the Strava writer. Set CHROME_PATH in .env when it is not on PATH.
CHROME_PATH ?= $(firstword $(shell command -v chromium chromium-browser google-chrome google-chrome-stable 2>/dev/null))
export CHROME_PATH

MOCK_DIR := .demo_data
REAL_DIR := .data

help: ## list the targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-14s %s\n", $$1, $$2}'

## ---- run it -------------------------------------------------------------

mock: ## fake Strava/Dexcom data, nothing leaves the machine (login demo@example.test)
	env -u GLUCAVA_SECRET_KEY -u GLUCAVA_TRUSTED_PROXIES GLUCAVA_DEMO=1 GLUCAVA_ADMIN_EMAIL=demo@example.test GLUCAVA_ADMIN_PASSWORD=$(DEMO_PW) \
		go run ./cmd/glucava serve --dev=false --dir $(MOCK_DIR) --http $(ADDR)

demo: mock ## alias for mock

dev: ## REAL Strava/Dexcom, go run (rebuilds on each start), data in .data
	@test -n "$(GLUCAVA_ADMIN_EMAIL)" -o -d $(REAL_DIR) || { echo "first run needs GLUCAVA_ADMIN_EMAIL and GLUCAVA_ADMIN_PASSWORD in .env"; exit 1; }
	go run ./cmd/glucava serve --dev=false --dir $(REAL_DIR) --http $(ADDR)

run: build ## REAL Strava/Dexcom, built binary (closest to production), data in .data
	@test -n "$(GLUCAVA_ADMIN_EMAIL)" -o -d $(REAL_DIR) || { echo "first run needs GLUCAVA_ADMIN_EMAIL and GLUCAVA_ADMIN_PASSWORD in .env"; exit 1; }
	./$(BIN) serve --dev=false --dir $(REAL_DIR) --http $(ADDR)

## ---- talk to the real data dir (.data) without the server ----------------

cli: build ## any command: make cli ARGS="strava cookies status"
	./$(BIN) $(ARGS) --dir $(REAL_DIR)

token: build ## create a trigger token: make token NAME=phone
	@test -n "$(NAME)" || { echo "usage: make token NAME=<label>"; exit 1; }
	./$(BIN) token create $(NAME) --dir $(REAL_DIR)

dexcom: build ## store Dexcom login: make dexcom DEXCOM_USER=me REGION=ous  (password is prompted)
	@test -n "$(DEXCOM_USER)" -a -n "$(REGION)" || { echo "usage: make dexcom DEXCOM_USER=<name> REGION=us|ous|jp"; exit 1; }
	@read -rs -p "Dexcom password: " pw; echo; printf '%s\n' "$$pw" | ./$(BIN) dexcom set "$(DEXCOM_USER)" --region $(REGION) --dir $(REAL_DIR)

strava-cookies: build ## import cookies: make strava-cookies FILE=cookies.json
	@test -n "$(FILE)" || { echo "usage: make strava-cookies FILE=cookies.json"; exit 1; }
	./$(BIN) strava cookies import $(FILE) --dir $(REAL_DIR)
	./$(BIN) strava cookies status --dir $(REAL_DIR)

strava-check: build ## dry run against Strava: make strava-check [ID=12345]  (never saves)
	./$(BIN) strava check $(ID) --dir $(REAL_DIR)

strava-photo: build ## dry run of the chart photo upload, three methods, never saves: make strava-photo ID=12345
	@test -n "$(ID)" || { echo "usage: make strava-photo ID=<activity id>"; exit 1; }
	./$(BIN) strava check $(ID) --photo --screenshot $(or $(SHOT),/tmp/glucava-photo.png) --dir $(REAL_DIR)

strava-hr: build ## read-only: where does Strava serve heart rate? make strava-hr ID=12345
	@test -n "$(ID)" || { echo "usage: make strava-hr ID=<activity id>"; exit 1; }
	./$(BIN) strava check $(ID) --hr --dir $(REAL_DIR)

strava-list: build ## raw recent-activities JSON: make strava-list
	./$(BIN) strava list --raw --dir $(REAL_DIR)

glucose-import: build ## import a CGM export: make glucose-import FILE=export.csv FORMAT=libre [SOURCE=libre]
	@test -n "$(FILE)" -a -n "$(FORMAT)" || { echo "usage: make glucose-import FILE=<path> FORMAT=libre|nightscout [SOURCE=label]"; exit 1; }
	./$(BIN) glucose import $(FILE) --format $(FORMAT) --source "$(if $(SOURCE),$(SOURCE),$(FORMAT))" --dir $(REAL_DIR)

shot: ## screenshots of a running mock into ./shots (needs CHROME_PATH)
	go run ./tools/shot -url http://$(ADDR) -out shots -password $(DEMO_PW)

mailshot: ## render sample notification emails to docs/img (needs CHROME_PATH)
	go run ./tools/mailshot -out docs/img

site: ## preview the project site on http://127.0.0.1:8000 (docs/site + docs/img)
	rm -rf .site && mkdir .site && cp -r docs/site/. .site/ && cp -r docs/img .site/img
	@echo "http://127.0.0.1:8000"; cd .site && python3 -m http.server 8000 --bind 127.0.0.1

## ---- quality ------------------------------------------------------------

build: ## compile ./glucava
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/glucava

test: ## unit tests with the race detector
	go test -race ./...

vet:
	go vet ./...

fmt: ## gofmt everything
	gofmt -w .

lint:
	golangci-lint run

vuln: ## govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: vet test lint vuln ## everything CI runs

hooks: ## enable the tracked git hooks (pre-commit: fmt, vet, lint; pre-push: tests, govulncheck)
	git config core.hooksPath .githooks

## ---- cleanup ------------------------------------------------------------

reset-mock: ## delete the mock data dir
	rm -rf $(MOCK_DIR)

reset-real: ## delete the REAL data dir (needs CONFIRM=1)
	@test "$(CONFIRM)" = 1 || { echo "this deletes $(REAL_DIR), including stored credentials. Repeat with CONFIRM=1"; exit 1; }
	rm -rf $(REAL_DIR)

clean: ## remove build output, mock data and screenshots (keeps .data)
	rm -rf $(BIN) $(MOCK_DIR) shots
