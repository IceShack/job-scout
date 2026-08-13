.PHONY: build run test vet docker kustomize-check

build:
	cd scraper && go build ./...

# Reads scraper/config.yaml — copy config.example.yaml over first.
run:
	cd scraper && go run ./cmd/scraper

test:
	cd scraper && go test ./...

vet:
	cd scraper && go vet ./...

docker:
	docker build -t ghcr.io/iceshack/job-scout/scraper:latest scraper

kustomize-check:
	kubectl kustomize k8s/base > /dev/null && echo "k8s/base OK"
	kubectl kustomize k8s/example-overlay > /dev/null && echo "k8s/example-overlay OK"
