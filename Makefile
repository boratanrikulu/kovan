.PHONY: build test vet fmt install

build:
	go build -o bin/kovan ./cmd/kovan

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
	goimports -w .

install:
	go install ./cmd/kovan
