.PHONY: build bundle bundle-linux-amd64 release-linux-amd64 verify-bundles test clean

build:
	./scripts/build.sh

bundle:
	./scripts/build-bundle.sh

bundle-linux-amd64:
	AGENT_HARBOR_BUNDLE_TARGETS=linux-amd64 ./scripts/build-bundle.sh

release-linux-amd64:
	./scripts/build-linux-amd64-release.sh

verify-bundles:
	./scripts/verify-bundles.sh

test:
	./scripts/build_test.sh
	cd launcher && go test ./...
	cd tui && go test ./internal/app ./internal/e2e ./internal/formui ./internal/managedconfig

clean:
	rm -rf dist
