default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -race -timeout=120s -parallel=10 ./...

testacc:
	@if [ -f .env ]; then \
		while IFS= read -r line || [ -n "$$line" ]; do \
			if [[ ! "$$line" =~ ^[[:space:]]*# ]] && [[ "$$line" =~ = ]]; then \
				key=$$(echo "$$line" | cut -d'=' -f1 | xargs); \
				val=$$(echo "$$line" | cut -d'=' -f2- | sed -e 's/^"//' -e 's/"$$//' -e "s/^'//" -e "s/'$$//"); \
				export "$$key"="$$val"; \
			fi; \
		done < .env; \
	fi; \
	export GITHUB_TOKEN=$${GITHUB_TOKEN:-$$(go run ./cmd/get-token 2>/dev/null)}; \
	export GITHUB_ENTERPRISE_SLUG=$${GITHUB_ENTERPRISE_SLUG:-$$TF_VAR_enterprise_slug}; \
	export GITHUB_TARGET_ORG=$${GITHUB_TARGET_ORG:-$$TF_VAR_target_org}; \
	export GITHUB_APP_CLIENT_ID=$${GITHUB_APP_CLIENT_ID:-$$TF_VAR_client_id}; \
	export GITHUB_TEST_REPO=$${GITHUB_TEST_REPO:-test-1}; \
	TF_ACC=1 go test -v -cover -race -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate
