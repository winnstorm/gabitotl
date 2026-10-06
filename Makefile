BIN := bin/gabitotl

.PHONY: build test vet fmt

build:
	go build -o $(BIN) ./cmd/gabitotl

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
