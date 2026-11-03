GO ?= go
VERSION ?= 0.2.0-dev
LDFLAGS = -s -w -buildid= -X main.version=$(VERSION)
# reproducible: no VCS stamp, no paths, no build id, no cgo
GOBUILD = CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false
.PHONY: build test vet e2e nginx-check dist check
build:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/csbroker ./cmd/csbroker
test:
	$(GO) test -count=1 ./internal/...
vet:
	$(GO) vet ./...
# in-process e2e (no root, no network): registration, approval, jump E2E, impostor, reconnect, bans, proxies, panel
e2e:
	$(GO) test -count=1 -v -timeout 10m ./e2e
# checks deploy/nginx/node.n3xus.ir.conf with a real local nginx (needs nginx, openssl, curl)
nginx-check:
	./e2e/nginx_check.sh
check: vet test e2e
dist:
	rm -rf dist; mkdir -p dist; for a in amd64 arm64; do GOOS=linux GOARCH=$$a $(GOBUILD) -ldflags "$(LDFLAGS)" -o dist/csbroker-linux-$$a ./cmd/csbroker; done
	cd dist && sha256sum csbroker-* > SHA256SUMS
