.PHONY: clean build test

export GO111MODULE = on

build: clean
	go build -o bin/akita .

clean:
	go clean

test:
	mockgen -source ./rest/interface.go -destination ./rest/mock/interface.mock.go -package mock
	go test ./...
