.PHONY: all build web test dist publish clean help desktop dmg install-cli install-desktop install-local

VERSION ?= $(shell cat VERSION 2>/dev/null | tr -d ' \r\n')
# Support 'make publish 0.2.0' where target argument is passed as next goal
VERSION_ARG := $(filter-out publish,$(MAKECMDGOALS))
ifeq ($(strip $(VERSION_ARG)),)
  TARGET_VERSION := $(VERSION)
else
  TARGET_VERSION := $(strip $(VERSION_ARG))
  # Treat extra goal as no-op so make doesn't attempt to build it
  %:
	@:
endif

# Clean leading 'v' from version string if present
CLEAN_VERSION := $(patsubst v%,%,$(TARGET_VERSION))

POSTHOG_KEY ?= $(PX0_POSTHOG_KEY)
LDFLAGS := -s -w
ifneq ($(strip $(POSTHOG_KEY)),)
  LDFLAGS += -X main.posthogKey=$(strip $(POSTHOG_KEY))
endif

all: build

help:
	@echo "px0 make targets:"
	@echo "  make build             - build px0 binary for current platform (optional: POSTHOG_KEY=phc_...)"
	@echo "  make web               - bundle web assets (JS/CSS/themes)"
	@echo "  make test              - run go test suite"
	@echo "  make dist              - compile cross-platform binaries into dist/"
	@echo "  make publish <version> - bump VERSION, commit, tag, and build dist binaries"
	@echo "  make desktop           - build native macOS .app with Wails"
	@echo "  make dmg               - wrap .app into installable .dmg"
	@echo "  make install-cli       - install px0 CLI to ~/.local/bin"
	@echo "  make install-desktop   - install px0.app to ~/Applications"
	@echo "  make install-local     - install both the CLI and native app"
	@echo "  make clean             - remove build artifacts"

web:
	@echo "Bundling web assets..."
	@node ./scripts/build-web.js

build: web
	@echo "Building px0 for local system..."
	go build -trimpath -ldflags="$(LDFLAGS)" -o px0 .
	@echo "Built ./px0 ($$(du -h px0 | cut -f1))"

test: web
	go test -v ./...

dist: web
	@./build.sh

publish:
	@if [ -z "$(CLEAN_VERSION)" ]; then \
		echo "Error: Version cannot be empty. Usage: make publish <version> (e.g. make publish 0.2.0)"; \
		exit 1; \
	fi
	@echo "==> Preparing release v$(CLEAN_VERSION) (previous: $$(cat VERSION))"
	@echo "$(CLEAN_VERSION)" > VERSION
	@echo "==> Bundling web assets and building dist binaries..."
	@./build.sh
	@echo "==> Updating git repository..."
	@git add VERSION
	@git commit -m "Release v$(CLEAN_VERSION)" || true
	@git tag -fa "v$(CLEAN_VERSION)" -m "Release v$(CLEAN_VERSION)"
	@echo ""
	@echo "✓ Successfully prepared release v$(CLEAN_VERSION) and updated dist/!"
	@echo "Tag v$(CLEAN_VERSION) has been created."
	@echo ""
	@echo "To publish to GitHub and trigger release workflow, run:"
	@echo "  git push origin master --tags"

desktop: web ## build the native macOS .app (requires wails in PATH)
	@echo "Building px0.app..."
	@PATH="$$HOME/go/bin:$$PATH" wails build -tags desktop -platform darwin/universal -clean -s -skipbindings -trimpath -ldflags="$(LDFLAGS)"
	@/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $(VERSION)" build/bin/px0.app/Contents/Info.plist
	@/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $(VERSION)" build/bin/px0.app/Contents/Info.plist
	@/usr/libexec/PlistBuddy -c "Set :CFBundleGetInfoString px0 $(VERSION)" build/bin/px0.app/Contents/Info.plist
	@codesign --force --deep --sign - build/bin/px0.app
	@codesign --verify --deep --strict build/bin/px0.app
	@echo "Built build/bin/px0.app"

dmg: desktop ## wrap the built .app into a .dmg (requires create-dmg)
	@rm -f build/bin/px0.dmg build/bin/rw.*.px0.dmg
	create-dmg \
	  --volname "px0" \
	  --window-size 540 380 \
	  --icon "px0.app" 140 180 \
	  --app-drop-link 400 180 \
	  --skip-jenkins \
	  "build/bin/px0.dmg" "build/bin/px0.app"
	@echo "Built build/bin/px0.dmg"

install-cli: build ## install the CLI used by px0 -d
	@mkdir -p "$(HOME)/.local/bin"
	@install -m 755 px0 "$(HOME)/.local/bin/px0"
	@echo "Installed $(HOME)/.local/bin/px0"

install-desktop: desktop ## install the native app for px0 -d
	@mkdir -p "$(HOME)/Applications"
	@rm -rf "$(HOME)/Applications/px0.app"
	@ditto build/bin/px0.app "$(HOME)/Applications/px0.app"
	@codesign --verify --deep --strict "$(HOME)/Applications/px0.app"
	@echo "Installed $(HOME)/Applications/px0.app"

install-local: install-cli install-desktop ## install the local native workflow

clean:
	rm -f px0
	rm -rf dist/ build/bin/
