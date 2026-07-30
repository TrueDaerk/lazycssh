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

BINARY  := lazycssh
PACKAGE := ./cmd/lazycssh
BINDIR  ?= $(HOME)/.local/bin
GO      ?= go

.PHONY: all build install uninstall clean test vet

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
