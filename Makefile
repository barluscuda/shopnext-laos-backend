.PHONY: fmt check integration dev migrate api worker build
fmt:
	gofmt -w cmd internal migrations tests
check: fmt
	go vet -tags nomsgpack ./...
	go test -tags nomsgpack ./...
integration:
	docker compose -f compose.test.yaml up -d --wait
	DATABASE_URL='postgres://shopnext:shopnext@localhost:55433/shopnext_test?sslmode=disable' go run ./cmd/migrate up
	TEST_DATABASE_URL='postgres://shopnext:shopnext@localhost:55433/shopnext_test?sslmode=disable' TEST_REDIS_URL='redis://localhost:56380/0' go test -tags nomsgpack -race ./tests/integration -count=1
dev:
	docker compose up -d --wait
migrate:
	go run ./cmd/migrate up
api:
	go run -tags nomsgpack ./cmd/api
worker:
	go run -tags nomsgpack ./cmd/worker
build:
	docker compose build
