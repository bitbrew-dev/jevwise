.EXPORT_ALL_VARIABLES:
NAME = jev
DirName ?= build
PKG = jev
ProjectUrl = "https://github.com/bitbrew-dev/jevwise"
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BuildTime = $(shell date -u '+%Y-%m-%d_%H:%M:%S')
BuildCommit = $(shell git rev-parse --short HEAD)
DEFAULT_CORES = 1
TREE_LEVEL ?= 5
NIGHTLY ?= 0

# Determine the OS and ARCH if not provided
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# ifdef NIGHTLY
ifeq ($(NIGHTLY),1)
    VERSION = $(shell git rev-parse --short HEAD)
    VERSION_TYPE = "nightly"
else
    VERSION = $(shell git describe --abbrev=0 --tags 2>/dev/null || echo $(shell git rev-parse --short HEAD))
    VERSION_TYPE = "latest release"
endif

# count cpu
ifeq ($(UNAME_S),Darwin)
    DEFAULT_CORES := $(shell sysctl -n hw.ncpu)
else
    DEFAULT_CORES := $(shell nproc)
endif

CORES ?= ${DEFAULT_CORES}

# Default OS and ARCH
ifeq ($(UNAME_S), Darwin)
	DEFAULT_GOOS = darwin
else ifeq ($(UNAME_S), Linux)
	DEFAULT_GOOS = linux
else ifeq ($(UNAME_S), Windows)
	DEFAULT_GOOS = windows
else
	DEFAULT_GOOS = $(UNAME_S)
endif

# Determine the ARCH if not provided
ifeq ($(UNAME_M), x86_64)
	DEFAULT_GOARCH = amd64
else ifeq ($(UNAME_M), arm64)
	DEFAULT_GOARCH = arm64
else
	DEFAULT_GOARCH = $(UNAME_M)
endif

.PHONY: verify
## Verify that NAME is defined
verify:
	@if [ -z "$(NAME)" ]; then \
		echo "Error: NAME is not defined. Please set NAME (e.g., make build NAME=obj_transform)"; \
		exit 1; \
	fi

.PHONY: build-platform
## Build for specified OS and ARCH
build-platform: verify
	@echo "Building $(VERSION_TYPE) version: $(VERSION)"
	@mkdir -p build
	@GOOS=$(GOOS) GOARCH=$(GOARCH) go build -p $(CORES) -v \
	        -o ./${DirName}/$(NAME)-$(GOOS)-$(GOARCH) \
		    -ldflags="-s -w \
		    -X ${PKG}/pkg/config.Version=${VERSION}  \
			-X ${PKG}/pkg/config.BuildTime=${BuildTime}  \
			-X ${PKG}/pkg/config.ProjectUrl=${ProjectUrl} " \
		    ./cmd && \
	chmod +x ./${DirName}/$(NAME)-$(GOOS)-$(GOARCH)
	@echo "Built $(NAME) for $(GOOS) $(GOARCH)"

.PHONY: build
## Build for all supported platforms and architectures
build: verify
	@$(MAKE) build-platform GOOS=darwin GOARCH=amd64
	@$(MAKE) build-platform GOOS=darwin GOARCH=arm64
	@$(MAKE) build-platform GOOS=linux GOARCH=amd64
	@$(MAKE) build-platform GOOS=linux GOARCH=arm64
	@$(MAKE) build-platform GOOS=windows GOARCH=amd64
	@$(MAKE) build-platform GOOS=windows GOARCH=arm64
	@echo "Built $(NAME) for all platforms"

.PHONY: clean
## Remove build files and caches
clean:
	@rm -rf ./$(DirName) 2>/dev/null || true
	@rm -f coverage.html coverage.out 2>/dev/null || true
	@go clean -cache
	@go clean -testcache
	@echo "Cleaned build artifacts and caches"

.PHONY: tree
## Print file structure
tree:
	@tree . -I 'artifacts' -I 'vendor' -I 'templates' -I 'logs' -I 'stacks' -I 'scripts' -I 'build' -L $(TREE_LEVEL)

.PHONY: bintest
## Copy the specified binary file to /usr/local/bin for testing
bintest: verify
	@ls ./$(DirName)/${NAME}-${GOOS}-${GOARCH} >/dev/null 2>&1 || (echo "binary doesn't exist!" && exit 1)
	@chmod +x ./${DirName}/${NAME}-${GOOS}-${GOARCH} # && echo "ensure binary is executable"
	@if [[ -f /usr/local/bin/${NAME} ]]; then sudo rm /usr/local/bin/${NAME}; fi
	@sudo cp ./${DirName}/${NAME}-${GOOS}-${GOARCH} /usr/local/bin/${NAME}
	@echo completion file is ready at /usr/local/bin/${NAME}

.PHONY: binrm
## Remove the binary
binrm: verify
	@rm -rf  /usr/local/bin/${NAME}


.DEFAULT_GOAL := help

help:
	@echo "$$(tput bold)Available rules:$$(tput sgr0)"
	@echo
	@sed -n -e "/^## / { \
		h; \
		s/.*//; \
		:doc" \
		-e "H; \
		n; \
		s/^## //; \
		t doc" \
		-e "s/:.*//; \
		G; \
		s/\\n## /---/; \
		s/\\n/ /g; \
		p; \
	}" ${MAKEFILE_LIST} \
	| LC_ALL='C' sort --ignore-case \
	| awk -F '---' \
		-v ncol=$$(tput cols) \
		-v indent=19 \
		-v col_on="$$(tput setaf 6)" \
		-v col_off="$$(tput sgr0)" \
	'{ \
		printf "%s%*s%s ", col_on, -indent, $$1, col_off; \
		n = split($$2, words, " "); \
		line_length = ncol - indent; \
		for (i = 1; i <= n; i++) { \
			line_length -= length(words[i]) + 1; \
			if (line_length <= 0) { \
				line_length = ncol - indent - length(words[i]) - 1; \
				printf "\n%*s ", -indent, " "; \
			} \
			printf "%s ", words[i]; \
		} \
		printf "\n"; \
	}' \
	| more $(shell test $(shell uname) = Darwin && echo '--no-init --raw-control-chars')
