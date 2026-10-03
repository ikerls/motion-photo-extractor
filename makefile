run:
	go run ./cmd/go-motion-photo

build:
	go build -o bin/go-motion-photo ./cmd/go-motion-photo

test:
	go test ./...

tidy:
	go mod tidy
	go mod vendor
