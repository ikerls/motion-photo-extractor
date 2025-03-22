run:
	go run ./cmd/cli/main.go

build:
	go build ./cmd/cli/main.go

tidy:
	go mod tidy
	go mod vendor
