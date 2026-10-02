.DEFAULT_GOAL := dev
.PHONY: fmt check integration dev migrate api worker staff build logs stop

# Do not let Compose implicitly open .env. Supply configuration via the shell.
COMPOSE_ENV_FILE ?= $(if $(wildcard .env.example),.env.example,/dev/null)
COMPOSE := docker compose --env-file $(COMPOSE_ENV_FILE)
export LOCAL_UID := $(shell id -u)
export LOCAL_GID := $(shell id -g)
export STAFF_EMAIL = $(EMAIL)
export STAFF_NAME = $(NAME)

fmt:
	$(COMPOSE) run --build --rm --no-deps tools gofmt -w cmd internal migrations tests
check:
	$(COMPOSE) run --build --rm --no-deps tools sh -c 'gofmt -w cmd internal migrations tests && go vet -tags nomsgpack ./... && go test -tags nomsgpack ./...'
integration:
	$(COMPOSE) -f compose.test.yaml run --build --rm tests
dev:
	$(COMPOSE) up --build -d --wait
migrate:
	$(COMPOSE) run --build --rm migrate
api:
	$(COMPOSE) up --build -d --wait api
worker:
	$(COMPOSE) up --build -d worker
staff:
	@test -n "$$STAFF_EMAIL" -a -n "$$STAFF_NAME" || { echo 'Usage: make staff EMAIL=owner@example.com NAME="Owner"'; exit 1; }
	$(COMPOSE) run --build --rm staff --email "$$STAFF_EMAIL" --name "$$STAFF_NAME"
build:
	$(COMPOSE) build
logs:
	$(COMPOSE) logs --follow api worker
stop:
	$(COMPOSE) stop
