.PHONY: check test api

GO_ENV = GOCACHE=/private/tmp/jianwo-go-cache

check:
	node miniapp/scripts/validate.mjs
	npm run check:runtime
	cd server && $(GO_ENV) go vet ./...

test:
	node miniapp/scripts/validate.mjs
	npm run build
	cd server && $(GO_ENV) go test ./...

api:
	cd server && $(GO_ENV) go run ./cmd/api
