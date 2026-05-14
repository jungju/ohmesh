SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

-include .env

APP_NAME ?= ohmesh
BIN_DIR ?= bin
TMP_DIR ?= tmp
PID_FILE ?= $(TMP_DIR)/ohmesh.pid
LOG_FILE ?= $(TMP_DIR)/ohmesh.log
GITHUB_OWNER ?= jungju
IMAGE ?= ghcr.io/$(GITHUB_OWNER)/ohmesh:main
K8S_NAMESPACE ?= ohmesh
AIR ?= air
AIR_INSTALL ?= github.com/air-verse/air@latest

OHMESH_ADDR ?= :8080
OHMESH_DATABASE_PATH ?= $(TMP_DIR)/ohmesh.db
OHMESH_SESSION_SECRET ?= local-dev-secret-change-me
OHMESH_SESSION_COOKIE ?= ohmesh_session
OHMESH_SESSION_TTL ?= 720h
OHMESH_COOKIE_SECURE ?= false

export OHMESH_ADDR
export OHMESH_DATABASE_PATH
export OHMESH_SESSION_SECRET
export OHMESH_SESSION_COOKIE
export OHMESH_SESSION_TTL
export OHMESH_COOKIE_SECURE
export OHMESH_ALLOWED_ORIGINS
export GITHUB_CLIENT_ID
export GITHUB_CLIENT_SECRET
export GOOGLE_CLIENT_ID
export GOOGLE_CLIENT_SECRET

.PHONY: help env deps fmt test build check install-air dev run start stop restart status logs health clean \
	k8s-deploy k8s-status k8s-logs k8s-delete k8s-port-forward k8s-oauth-secret k8s-ghcr-secret package-watch

help:
	@echo "ohmesh local commands"
	@echo
	@echo "  make env       Create .env from .env.example if missing"
	@echo "  make deps      Download and tidy Go dependencies"
	@echo "  make fmt       Format Go files"
	@echo "  make test      Run Go tests"
	@echo "  make build     Build $(BIN_DIR)/$(APP_NAME)"
	@echo "  make check     Run fmt, test, and build"
	@echo "  make install-air Install the Air live-reload tool"
	@echo "  make dev       Run the API with Air live reload"
	@echo "  make run       Run the API in the foreground"
	@echo "  make start     Build and run the API in the background"
	@echo "  make stop      Stop the background API"
	@echo "  make restart   Restart the background API"
	@echo "  make status    Show background API status"
	@echo "  make logs      Tail background API logs"
	@echo "  make health    Check /healthz"
	@echo "  make clean     Remove local build and runtime files"
	@echo "  make k8s-deploy       Deploy to Kubernetes with kubectl"
	@echo "  make k8s-status       Show Kubernetes resources"
	@echo "  make k8s-logs         Tail Kubernetes deployment logs"
	@echo "  make k8s-port-forward Port-forward Kubernetes service to localhost:8080"
	@echo "  make k8s-delete       Delete Kubernetes resources"
	@echo "  make k8s-ghcr-secret  Create/update GHCR pull secret named github"
	@echo "  make package-watch    Watch the latest container build workflow"

env:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "Created .env from .env.example"; \
	else \
		echo ".env already exists"; \
	fi

deps:
	go mod tidy

fmt:
	gofmt -w cmd internal

test:
	go test ./...

build:
	@mkdir -p "$(BIN_DIR)"
	go build -o "$(BIN_DIR)/$(APP_NAME)" ./cmd/ohmesh

check: fmt test build

install-air:
	go install "$(AIR_INSTALL)"

dev: env
	@mkdir -p "$(TMP_DIR)"
	@if command -v "$(AIR)" >/dev/null 2>&1; then \
		"$(AIR)" -c .air.toml; \
	else \
		echo "Air is not installed. Run: make install-air"; \
		exit 1; \
	fi

run: env
	@mkdir -p "$(TMP_DIR)"
	go run ./cmd/ohmesh

start: env build
	@mkdir -p "$(TMP_DIR)"
	@if [ -f "$(PID_FILE)" ] && kill -0 "$$(cat "$(PID_FILE)")" 2>/dev/null; then \
		echo "ohmesh is already running with PID $$(cat "$(PID_FILE)")"; \
	else \
		port="$${OHMESH_ADDR##*:}"; \
		if command -v lsof >/dev/null 2>&1 && lsof -ti tcp:"$$port" -sTCP:LISTEN >/dev/null 2>&1; then \
			echo "Port $$port is already in use. Stop that process or run with OHMESH_ADDR=:8082."; \
			exit 1; \
		fi; \
		if command -v setsid >/dev/null 2>&1; then \
			setsid "$(BIN_DIR)/$(APP_NAME)" >"$(LOG_FILE)" 2>&1 < /dev/null & \
		else \
			nohup "$(BIN_DIR)/$(APP_NAME)" >"$(LOG_FILE)" 2>&1 & \
		fi; \
		echo $$! >"$(PID_FILE)"; \
		sleep 0.5; \
		if ! kill -0 "$$(cat "$(PID_FILE)")" 2>/dev/null; then \
			echo "ohmesh failed to start. Logs:"; \
			tail -n 40 "$(LOG_FILE)" || true; \
			rm -f "$(PID_FILE)"; \
			exit 1; \
		fi; \
		echo "Started ohmesh with PID $$(cat "$(PID_FILE)")"; \
		echo "Logs: $(LOG_FILE)"; \
		if [[ "$(OHMESH_ADDR)" == :* ]]; then \
			echo "URL: http://localhost$(OHMESH_ADDR)"; \
		else \
			echo "URL: http://$(OHMESH_ADDR)"; \
		fi; \
	fi

stop:
	@if [ -f "$(PID_FILE)" ] && kill -0 "$$(cat "$(PID_FILE)")" 2>/dev/null; then \
		kill "$$(cat "$(PID_FILE)")"; \
		rm -f "$(PID_FILE)"; \
		echo "Stopped ohmesh"; \
	else \
		rm -f "$(PID_FILE)"; \
		echo "ohmesh is not running"; \
	fi

restart: stop start

status:
	@if [ -f "$(PID_FILE)" ] && kill -0 "$$(cat "$(PID_FILE)")" 2>/dev/null; then \
		echo "ohmesh is running with PID $$(cat "$(PID_FILE)")"; \
		if [[ "$(OHMESH_ADDR)" == :* ]]; then \
			echo "URL: http://localhost$(OHMESH_ADDR)"; \
		else \
			echo "URL: http://$(OHMESH_ADDR)"; \
		fi; \
	elif command -v lsof >/dev/null 2>&1 && lsof -ti tcp:"$${OHMESH_ADDR##*:}" -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port $${OHMESH_ADDR##*:} is in use, but $(PID_FILE) does not point to a running ohmesh process"; \
	else \
		echo "ohmesh is not running"; \
	fi

logs:
	@mkdir -p "$(TMP_DIR)"
	@touch "$(LOG_FILE)"
	tail -f "$(LOG_FILE)"

health:
	@url="http://$(OHMESH_ADDR)"; \
	if [[ "$(OHMESH_ADDR)" == :* ]]; then url="http://localhost$(OHMESH_ADDR)"; fi; \
	curl -fsS "$$url/healthz"; \
	echo

clean: stop
	rm -rf "$(BIN_DIR)"
	rm -f "$(TMP_DIR)"/ohmesh.db "$(TMP_DIR)"/ohmesh.db-* "$(LOG_FILE)"

k8s-deploy:
	kubectl apply -k deploy/k8s
	kubectl -n "$(K8S_NAMESPACE)" rollout status deploy/ohmesh --timeout=180s

k8s-status:
	kubectl -n "$(K8S_NAMESPACE)" get deploy,po,svc,ingress,pvc

k8s-logs:
	kubectl -n "$(K8S_NAMESPACE)" logs deploy/ohmesh -f

k8s-delete:
	kubectl delete -k deploy/k8s

k8s-port-forward:
	kubectl -n "$(K8S_NAMESPACE)" port-forward svc/ohmesh 8080:80

k8s-oauth-secret:
	kubectl create namespace "$(K8S_NAMESPACE)" --dry-run=client -o yaml | kubectl apply -f -
	kubectl -n "$(K8S_NAMESPACE)" create secret generic ohmesh-oauth \
		--from-literal=GITHUB_CLIENT_ID="$${GITHUB_CLIENT_ID:-}" \
		--from-literal=GITHUB_CLIENT_SECRET="$${GITHUB_CLIENT_SECRET:-}" \
		--from-literal=GOOGLE_CLIENT_ID="$${GOOGLE_CLIENT_ID:-}" \
		--from-literal=GOOGLE_CLIENT_SECRET="$${GOOGLE_CLIENT_SECRET:-}" \
		--dry-run=client -o yaml | kubectl apply -f -

k8s-ghcr-secret:
	kubectl create namespace "$(K8S_NAMESPACE)" --dry-run=client -o yaml | kubectl apply -f -
	kubectl -n "$(K8S_NAMESPACE)" create secret docker-registry github \
		--docker-server=ghcr.io \
		--docker-username="$(GITHUB_OWNER)" \
		--docker-password="$$(gh auth token)" \
		--dry-run=client -o yaml | kubectl apply -f -

package-watch:
	gh run watch "$$(gh run list --workflow 'Build container' --limit 1 --json databaseId --jq '.[0].databaseId')"
