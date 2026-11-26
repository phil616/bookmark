.PHONY: help build frontend backend test check web-test release clean

help:
	@printf '%s\n' 'make build     Build web/dist and bin/online-bookmarks' 'make test      Run Go tests' 'make check     Check versions, docs, Go tests/race/vet and frontend' 'make web-test  Install Chromium and run browser tests' 'make release   Build native/source archives and SHA256SUMS in dist/' 'make clean     Remove generated build and test output'

build: frontend backend

frontend:
	npm --prefix web ci
	npm --prefix web run build

backend:
	mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o bin/online-bookmarks ./cmd/server

test:
	go test ./...

check:
	python3 scripts/release.py --check
	go test -race ./...
	go vet ./...
	$(MAKE) frontend

web-test:
	npm --prefix web ci
	npm --prefix web run build
	cd web && npx playwright install chromium && npx playwright test

release:
	python3 scripts/release.py

clean:
	rm -rf bin dist web/dist web/test-results web/playwright-report web/tsconfig.tsbuildinfo
