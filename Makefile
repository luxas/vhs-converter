all: test

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

build:
	go build -o bin/vhs-converter cmd/vhs-converter

test: build tidy fmt vet
	go test ./... -coverprofile cover.out
