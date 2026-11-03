APP := wada-terminal

.PHONY: run build build-dev test vet check fmt

run:
	MYGO_ENV=development go run .

build:
	go build -ldflags '-X github.com/egoist/mygo.production=1' -o dist/prod/$(APP) .

build-dev:
	go build -o dist/dev/$(APP) .

test:
	go test ./...

vet:
	go vet ./...
	go tool mygo vet .

fmt:
	gofmt -l -w .

check: vet test
