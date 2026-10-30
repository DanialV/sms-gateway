TEST_MYSQL_DSN ?= sms:sms@tcp(127.0.0.1:3307)/sms_test?parseTime=true&loc=UTC

export CGO_ENABLED = 0

.PHONY: build test test-integration up down logs

build:
	go build -o bin/receiver ./cmd/receiver
	go build -o bin/worker ./cmd/worker

test:
	go test ./...

test-integration:
	TEST_MYSQL_DSN='$(TEST_MYSQL_DSN)' go test -count=1 ./internal/adapter/mysql/...

up:
	docker compose up -d --build

down:
	docker compose down -v

logs:
	docker compose logs -f receiver worker
