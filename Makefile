APP_NAME := sudoku4x4
NPM_INSTALL ?= npm install

PLATFORM ?= $(shell go env GOOS)/$(shell go env GOARCH)
GOOS := $(word 1,$(subst /, ,$(PLATFORM)))
GOARCH := $(word 2,$(subst /, ,$(PLATFORM)))
PLATFORM_DIR := $(GOOS)-$(GOARCH)

ifeq ($(GOOS),windows)
GO_BIN := $(shell go env GOPATH)\\bin
WAILS := wails3
WAILS_RUN := $(WAILS)
else
GO_BIN := $(shell go env GOPATH)/bin
WAILS := $(or $(shell command -v wails3 2>/dev/null),$(GO_BIN)/wails3)
WAILS_RUN := PATH="$(GO_BIN):$$PATH" $(WAILS)
endif
ifeq ($(GOOS),windows)
OUTPUT_BIN := build/bin/$(APP_NAME).exe
else
OUTPUT_BIN := build/bin/$(APP_NAME)
endif

HEV_VERSION ?= 2.14.4

.PHONY: help frontend core core-clean-foreign core-sudoku core-hev dev build bundle

ensure-wails:
ifeq ($(GOOS),windows)
	@powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "if (-not (Get-Command wails3 -ErrorAction SilentlyContinue)) { Write-Host 'wails3 CLI not found in PATH. Install: go install github.com/wailsapp/wails/v3/cmd/wails3@latest'; exit 1 }"
else
	@if [ ! -x "$(WAILS)" ]; then \
		echo "wails3 CLI not found in PATH or $(GO_BIN). Install: go install github.com/wailsapp/wails/v3/cmd/wails3@latest"; \
		exit 1; \
	fi
endif

help:
	@echo "Targets:"
	@echo "  make dev        Run wails3 dev (ensures core binaries)"
	@echo "  make build      Build app + bundle core binaries"
	@echo "  make core       Prepare core binaries under ./runtime/bin/<os>-<arch>/"
	@echo "  make frontend   npm ci + npm run build"

frontend:
	cd frontend && $(NPM_INSTALL) && npm run build

ifeq ($(GOOS),windows)
core:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/core.ps1 -What all
else
core: core-clean-foreign core-sudoku core-hev
endif

core-clean-foreign:
ifeq ($(GOOS),windows)
	@powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "New-Item -ItemType Directory -Force -Path 'runtime/bin' | Out-Null; Get-ChildItem -Path 'runtime/bin' -Directory -ErrorAction SilentlyContinue | Where-Object { $$_.Name -ne '$(PLATFORM_DIR)' } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
else
	@mkdir -p runtime/bin
	@find runtime/bin -mindepth 1 -maxdepth 1 -type d ! -name "$(PLATFORM_DIR)" -exec rm -rf {} +
endif

core-sudoku:
ifeq ($(GOOS),windows)
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/core.ps1 -What sudoku
else
	GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/build_sudoku_target.sh
endif

core-hev:
ifeq ($(GOOS),windows)
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/core.ps1 -What hev
else
	HEV_VERSION=$(HEV_VERSION) GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/fetch_hev_release.sh
endif

dev: ensure-wails core
	$(WAILS_RUN) dev -config ./build/config.yml

build: ensure-wails frontend core
ifeq ($(GOOS),darwin)
	$(WAILS_RUN) task darwin:package ARCH=$(GOARCH) OUTPUT=build/bin/$(APP_NAME)
else
	$(WAILS_RUN) task $(GOOS):build ARCH=$(GOARCH) OUTPUT=$(OUTPUT_BIN)
endif
ifeq ($(GOOS),windows)
	@echo "[ok] Runtime binaries are embedded into executable for $(PLATFORM_DIR); skip filesystem bundle."
else
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh
endif

bundle: core
ifeq ($(GOOS),windows)
	@echo "[ok] Runtime binaries are embedded into executable for $(PLATFORM_DIR); skip filesystem bundle."
else
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh
endif
