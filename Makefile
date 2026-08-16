.PHONY: build test generate update-spec check

build:
	go build -o bin/cashctrl ./cmd/cashctrl

test:
	go test ./...

generate:
	go run ./tools/genmanifest

update-spec:
	go run ./tools/fetchspec

check:
	go vet ./...
	go run ./tools/genmanifest -check
	go test ./...
