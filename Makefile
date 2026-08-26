.PHONY: build test vet lint clean install fmt

BINARY := cardinal-wings

build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run 2>/dev/null || echo "golangci-lint not installed (optional)"

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)

install: build
	install -m 0755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "installed $(BINARY) to /usr/local/bin"