GO ?= go
BIN ?= parley

.PHONY: build fmt fmt-check vet lint test race check vuln hooks clean

build:
	$(GO) build -trimpath -o $(BIN) .

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

check: fmt-check vet lint test
	@echo "check: ok"

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

hooks:
	git config core.hooksPath .githooks
	@echo "git hooks enabled from .githooks"

clean:
	rm -f $(BIN)
