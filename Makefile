.PHONY: hooks build test vet fmt lint vuln check demo shot clean

BIN := glucava
DEMO_PW ?= demo-password-123

# Use the tracked git hooks (pre-commit: fmt, vet, lint; pre-push: tests, govulncheck).
hooks:
	git config core.hooksPath .githooks

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/glucava

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Everything CI runs.
check: vet test lint vuln

# Run with fake Strava/Dexcom data at http://127.0.0.1:8090
demo:
	GLUCAVA_DEMO=1 GLUCAVA_ADMIN_EMAIL=demo@example.test GLUCAVA_ADMIN_PASSWORD=$(DEMO_PW) \
		go run ./cmd/glucava serve --dir .demo_data --http 127.0.0.1:8090

# Screenshots of the running demo into ./shots (needs CHROME_PATH)
shot:
	go run ./tools/shot -url http://127.0.0.1:8090 -out shots -password $(DEMO_PW)

clean:
	rm -rf $(BIN) .demo_data shots
