APP_NAME := sudoku-desktop
WAILS := wails
NPM_INSTALL ?= npm install

PLATFORM ?= $(shell go env GOOS)/$(shell go env GOARCH)
GOOS := $(word 1,$(subst /, ,$(PLATFORM)))
GOARCH := $(word 2,$(subst /, ,$(PLATFORM)))
PLATFORM_DIR := $(GOOS)-$(GOARCH)

HEV_VERSION ?= 2.14.4

.PHONY: help frontend core core-sudoku core-hev dev build bundle

help:
	@echo "Targets:"
	@echo "  make dev        Run wails dev (ensures core binaries)"
	@echo "  make build      Build host app + bundle core binaries"
	@echo "  make core       Prepare core binaries under ./runtime/bin/<os>-<arch>/"
	@echo "  make frontend   npm ci + npm run build"

frontend:
	cd frontend && $(NPM_INSTALL) && npm run build

core: core-sudoku core-hev

core-sudoku:
	GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/build_sudoku_target.sh

core-hev:
	HEV_VERSION=$(HEV_VERSION) GOOS=$(GOOS) GOARCH=$(GOARCH) ./scripts/fetch_hev_release.sh

dev: core
	$(WAILS) dev

build: frontend core
	$(WAILS) build -clean -s -platform $(PLATFORM) -o $(APP_NAME)
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh

bundle: core
	PLATFORM=$(PLATFORM) ./scripts/bundle_runtime_into_build.sh
