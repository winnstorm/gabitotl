BIN := bin/gabitotl

.PHONY: build test e2e vet fmt fmt-check check clean

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
	rm -rf bin
