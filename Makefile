VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/shreyas-shriyan/loggy/internal/cli.Version=$(VERSION)
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test vet fmt cross clean snapshot release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/loggy ./cmd/loggy

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/loggy-$$os-$$arch$$ext ./cmd/loggy || exit 1; \
	done

clean:
	rm -rf bin dist

# Local test build (no publish, no tag required)
snapshot:
	goreleaser release --snapshot --clean

# Tag and push a release (triggers GitHub Actions automatically)
# Usage: make release TAG=v0.1.0
release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v0.1.0"; exit 1; fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)
