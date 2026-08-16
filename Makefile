.PHONY: run test fmt vet up down

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

up:
	docker compose up --build

down:
	docker compose down
