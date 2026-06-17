.PHONY: dev-db backend-test frontend-build

dev-db:
	docker compose up -d postgres

backend-test:
	cd backend && go test ./...

frontend-build:
	cd frontend && npm run build
