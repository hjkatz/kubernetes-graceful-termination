SHELL:=/usr/bin/env bash
REPO?=hjkatz/kubernetes-graceful-termination
PRJ_SRC_PATH:=github.com/hjkatz/kubernetes-graceful-termination
BIN_NAME?=graceful-terminator
CGO_ENABLED?=0
GOOS?=linux
GOARCH?=amd64
VERSION?=development
COMMIT_SHA?=$(shell git rev-parse --short HEAD)
BIN_PATH_LINUX:=bin/linux/amd64/
BIN_PATH_M1:=bin/darwin/arm64/

allPkgs = $(shell go list ./...)
allSrcDirs = cmd pkg tools

ifndef GOPATH
export GOPATH=$(shell go env "GOPATH")
endif
export GO111MODULE=on

.PHONY: all
all: test build

.PHONY: fmt
fmt:
	gofmt -s -w $(allSrcDirs)

# attempts to build all deps, and env correctly for what's in go.mod
.PHONY: gomod-clean
gomod-clean: clean gomod

# attempts to fix go mod
.PHONY: gomod
gomod:
	@echo "Running go mod tidy ; go mod vendor until successful (ctrl-c to stop)"
	@false ; while [[ $$? != 0 ]] ; do \
		go mod tidy && go mod vendor; \
	done;
	@echo "Done."

.PHONY: build
build: build-linux build-m1

.PHONY: build-linux
build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o ${BIN_PATH_LINUX}/${BIN_NAME} main.go

.PHONY: build-m1
build-m1:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=${CGO_ENABLED} go build -o ${BIN_PATH_M1}/${BIN_NAME} main.go

.PHONY: install
install:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=${CGO_ENABLED} go install main.go

.PHONY: test
test:
	go test -cover -mod=vendor ./...

.PHONY: clean
clean:
	rm -rf ./bin/
	go clean -testcache -modcache

.PHONY: container
container: build-linux
	docker build -t ${REPO}:${COMMIT_SHA} -f Dockerfile .
	docker tag ${REPO}:${COMMIT_SHA} ${REPO}:local
	@echo "Built ${REPO}:local"
