BIN := bin/gabitotl

.PHONY: build test e2e vet fmt fmt-check check clean manager-windows manager-icon

build:
	go build -o $(BIN) ./cmd/gabitotl

# All tests, including the end-to-end suite in internal/e2e.
test:
	go test ./...

# Only the end-to-end suite (builds the real binary, runs it against a fake machine).
e2e:
	go test -count=1 ./internal/e2e/...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed on:"; echo "$$unformatted"; exit 1; fi

# What CI runs.
check: fmt-check vet build test

clean:
	rm -rf bin dist cmd/gabitotl-manager/bundled

# Windows single-file setup: the manager with the CLI embedded.
# Override the version with `make manager-windows VERSION=1.2.3`.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo 0.0.0-dev)
VPKG := github.com/winnstorm/gabitotl/internal/version.Version
MGR := cmd/gabitotl-manager

manager-windows:
	mkdir -p dist $(MGR)/bundled
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X $(VPKG)=$(VERSION)" -o dist/gabitotl.exe ./cmd/gabitotl
	cp dist/gabitotl.exe $(MGR)/bundled/gabitotl.exe
	GOOS=windows GOARCH=amd64 go build -trimpath -tags desktop,production,embedcli -ldflags "-H windowsgui -s -w -X $(VPKG)=$(VERSION)" -o dist/GabitoTL-Setup-$(VERSION)-windows-amd64.exe ./$(MGR)

# Regenerates the manager icon and the Windows resource object.
manager-icon:
	go generate ./$(MGR)/internal/icongen/...
