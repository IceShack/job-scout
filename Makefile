.PHONY: build run test vet docker kustomize-check openapi

build:
	cd scraper && go build ./...

# Reads scraper/config.yaml — copy config.example.yaml over first.
run:
	cd scraper && go run ./cmd/scraper

test:
	cd scraper && go test ./...

vet:
	cd scraper && go vet ./...

# Regenerate the checked-in API document. The running server serves the
# same thing at /openapi.json; this copy is for reading on GitHub, and CI
# fails when it is stale.
openapi:
	cd scraper && go run ./cmd/scraper -openapi > ../docs/openapi.json

docker:
	docker build -t ghcr.io/iceshack/job-scout/scraper:latest scraper

kustomize-check:
	kubectl kustomize k8s/base > /dev/null && echo "k8s/base OK"
	kubectl kustomize k8s/example-overlay > /dev/null && echo "k8s/example-overlay OK"
