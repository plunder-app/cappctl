
SHELL := /bin/bash

# The name of the executable (default is current directory name)
TARGET := cappctl
.DEFAULT_GOAL: $(TARGET)

# These will be provided to the target
VERSION := 0.0.1
BUILD := `git rev-parse HEAD`

# Operating System Default (LINUX)
TARGETOS=linux

# Use linker flags to provide version/build settings to the target
LDFLAGS=-ldflags "-X=main.Version=$(VERSION) -X=main.Build=$(BUILD) -s"

# go source files, ignore vendor directory
SRC = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

DOCKERTAG=latest

.PHONY: all build clean install uninstall fmt simplify check run

all: check install

$(TARGET): $(SRC)
	@go build $(LDFLAGS) -o $(TARGET)

build: $(TARGET)
	@true

clean:
	@rm -f $(TARGET)

install:
	@echo Building and Installing cappctl
	@go install $(LDFLAGS)

uninstall: clean
	@rm -f $$(which ${TARGET})

fmt:
	@gofmt -l -w $(SRC)

docker:
	@GOOS=$(TARGETOS) make build
	@mv $(TARGET) ./dockerfile
	@docker build -t $(TARGET):$(DOCKERTAG) ./dockerfile/
	@rm ./dockerfile/$(TARGET)
	@echo New Docker image created

release_darwin:
	@echo Creating Darwin Build
	@GOOS=darwin make build
	@zip -9 -r cappctl-darwin-$(VERSION).zip ./cappctl
	@rm cappctl


release_linux:
	@echo Creating Linux Build
	@GOOS=linux make build
	@zip -9 -r cappctl-linux-$(VERSION).zip ./cappctl
	@rm cappctl

windows:
	@echo Creating Linux Build
	@GOOS=windows make build
	@zip -9 -r cappctl-win-$(VERSION).zip ./cappctl
	@rm cappctl

simplify:
	@gofmt -s -l -w $(SRC)

check:
	@test -z $(shell gofmt -l main.go | tee /dev/stderr) || echo "[WARN] Fix formatting issues with 'make fmt'"
	@for d in $$(go list ./... | grep -v /vendor/); do golint $${d}; done
	@go tool vet ${SRC}

run: install
	@$(TARGET)
