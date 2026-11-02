APP := wada-terminal

.PHONY: run build test vet check fmt

run:
	go run .

build:
	go build -o $(APP) .

test:
	go test ./...

vet:
	go vet ./...
	go tool mygo vet .

fmt:
	gofmt -l -w .

check: vet test
