export CGO_ENABLED := 0

BUILD_DIR ?= build
VERSION ?=
ARCH ?=

.PHONY: check test debug release publish icons screenshots clean

check:
	@for tool in golangci-lint govulncheck gosec; do \
		command -v $$tool >/dev/null || { echo "check needs $$tool on PATH" >&2; exit 1; }; \
	done
	@unformatted=$$(gofmt -l .); \
		if [ -n "$$unformatted" ]; then echo "gofmt would change:" >&2; echo "$$unformatted" >&2; exit 1; fi
	golangci-lint run --no-config ./...
	govulncheck ./...
	gosec -quiet -tests -exclude-generated -exclude=G204 ./...
	go tool mygo i18n check
	@for script in scripts/*.sh scripts/lib/*.sh; do bash -n "$$script" || exit 1; done

test:
	go test -race ./...
	go tool mygo vet .

debug:
	scripts/build-debug.sh $(if $(ARCH),--arch $(ARCH))

release:
	scripts/build-release.sh $(VERSION) $(if $(ARCH),--arch $(ARCH))

publish:
	scripts/publish.sh $(VERSION) $(if $(filter 1 true yes,$(DRY_RUN)),--dry-run)

icons:
	scripts/render-icons.sh

screenshots:
	scripts/screenshots.sh

clean:
	@case "$(strip $(BUILD_DIR))" in \
		"" | / | . | ..) echo 'BUILD_DIR must name a build directory.' >&2; exit 1 ;; \
	esac
	rm -rf -- "$(BUILD_DIR)"
