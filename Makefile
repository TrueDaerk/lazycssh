# Makefile for lazycssh — build and install the parallel SSH TUI.
#
# Usage:
#   make                                # build ./lazycssh
#   make install                        # install to ~/.local/bin/lazycssh
#   make install BINDIR=/usr/local/bin  # or pick another directory
#   make uninstall
#   make clean
#   make test
#   make vet
#   make docs                           # build the docs site strictly into ./site
#   make docs-deploy                    # publish the docs site to the gh-pages branch

BINARY  := lazycssh
PACKAGE := ./cmd/lazycssh
BINDIR  ?= $(HOME)/.local/bin
GO      ?= go
MKDOCS  ?= mkdocs

.PHONY: all build install uninstall clean test vet docs docs-deploy docs-check-mkdocs

all: build

build:
	$(GO) build -o $(BINARY) $(PACKAGE)

# Build straight into BINDIR rather than `go install`, so the destination is
# the user's own bin directory instead of whatever GOPATH points at.
install:
	mkdir -p $(BINDIR)
	$(GO) build -o $(BINDIR)/$(BINARY) $(PACKAGE)

uninstall:
	rm -f $(BINDIR)/$(BINARY)

clean:
	rm -f $(BINARY)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# `mkdocs build --strict` renders userdocs/ into ./site (git-ignored) and fails
# on any broken link, matching the strict: true intent in mkdocs.yml.
docs: docs-check-mkdocs
	$(MKDOCS) build --strict

# No GitHub Actions workflow publishes the docs site (removed in #287); this
# target is the manual replacement. `gh-deploy` builds and force-pushes the
# rendered site to the gh-pages branch, which GitHub Pages is configured to
# serve directly (see wiki/contributing/documentation.md).
docs-deploy: docs-check-mkdocs
	$(MKDOCS) gh-deploy --strict

docs-check-mkdocs:
	@command -v $(MKDOCS) >/dev/null 2>&1 || { \
		echo "error: mkdocs not found — install it with 'pip install mkdocs-material' (see userdocs/requirements.txt)" >&2; \
		exit 1; \
	}
