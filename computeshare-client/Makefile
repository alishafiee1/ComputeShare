GO ?= go
VERSION ?= 0.2.0-dev
LDFLAGS = -s -w -buildid= -X main.version=$(VERSION)
GOBUILD = CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false
.PHONY: build test vet e2e dist
build:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/ ./cmd/...
test:
	$(GO) test -count=1 ./...
vet:
	$(GO) vet ./...
# full-system e2e: needs root (sudo), Incus, sshpass and ../computeshare-server (see e2e/system_e2e.sh)
e2e:
	./e2e/system_e2e.sh
dist:
	rm -rf dist; mkdir -p dist; for t in linux/amd64 linux/arm64 windows/amd64; do o=$${t%/*}; a=$${t#*/}; x=; [ $$o = windows ] && x=.exe; \
	  for c in csnode cs csnode-authkeys cs-bench; do [ $$o = windows ] && { [ $$c = csnode-authkeys ] || [ $$c = cs-bench ]; } && continue; \
	  GOOS=$$o GOARCH=$$a $(GOBUILD) -ldflags "$(LDFLAGS)" -o dist/$$c-$$o-$$a$$x ./cmd/$$c; done; done
	K="$$(cat internal/release/release.pub)" awk '/^RELEASE_PUB=""$$/{print "RELEASE_PUB=\"" ENVIRON["K"] "\""; next} {print}' installer/linux/install.sh > dist/install.sh
	cd dist && sha256sum * > SHA256SUMS
