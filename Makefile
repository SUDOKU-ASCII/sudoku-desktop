APP_NAME := sudoku4x4
GO_BIN := $(shell go env GOPATH)/bin
WAILS := $(or $(shell command -v wails3 2>/dev/null),$(GO_BIN)/wails3)
WAILS_RUN := PATH="$(GO_BIN):$$PATH" $(WAILS)
NPM_INSTALL ?= npm install

PLATFORM ?= $(shell go env GOOS)/$(shell go env GOARCH)
GOOS := $(word 1,$(subst /, ,$(PLATFORM)))
GOARCH := $(word 2,$(subst /, ,$(PLATFORM)))
PLATFORM_DIR := $(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
OUTPUT_BIN := build/bin/$(APP_NAME).exe
else
OUTPUT_BIN := build/bin/$(APP_NAME)
endif

HEV_VERSION ?= 2.14.4

.PHONY: help frontend core core-clean-foreign core-sudoku core-hev dev build bundle

ensure-wails:
	@if [ ! -x "$(WAILS)" ]; then \
		echo "wails3 CLI not found in PATH or $(GO_BIN). Install: go install github.com/wailsapp/wails/v3/cmd/wails3@latest"; \
		exit 1; \
	fi

help:
	@echo "Targets:"
	@echo "  make dev        Run wails3 dev (ensures core binaries)"
	@echo "  make build      Build app + bundle core binaries"
	@echo "  make core       Prepare core binaries under ./runtime/bin/<os>-<arch>/"
	@echo "  make frontend   npm ci + npm run build"

frontend:
	cd frontend && $(NPM_INSTALL) && npm run build

core: core-clean-foreign core-sudoku core-hev

core-clean-foreign:
	@mkdir -p runtime/bin
	@find runtime/bin -mindepth 1 -maxdepth 1 -type d ! -name "$(PLATFORM_DIR)" -exec rm -rf {} +

core-sudoku:
	GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/build_sudoku_target.sh

core-hev:
	HEV_VERSION=$(HEV_VERSION) GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/fetch_hev_release.sh

dev: ensure-wails core
	$(WAILS_RUN) dev -config ./build/config.yml

build: ensure-wails frontend core
ifeq ($(GOOS),darwin)
	$(WAILS_RUN) task darwin:package ARCH=$(GOARCH) OUTPUT=build/bin/$(APP_NAME)
else
	$(WAILS_RUN) task $(GOOS):build ARCH=$(GOARCH) OUTPUT=$(OUTPUT_BIN)
endif
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh

bundle: core
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh
