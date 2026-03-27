BINARY   := lx
VERSION  := $(shell grep 'version.*=' cmd/root.go | head -1 | sed 's/.*"\(.*\)"/\1/')
PREFIX   ?= /usr/local

.PHONY: build install uninstall test clean version

build:
	go build -o $(BINARY) .

install: build
	install -Dm755 $(BINARY) $(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)

version:
	@echo $(VERSION)
