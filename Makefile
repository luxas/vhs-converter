UID_GID ?= $(shell id -u):$(shell id -g)
GO_VERSION ?= 1.15.6
PROJECT := github.com/luxas/digitized
BOUNDING_API_DIRS := ${PROJECT}/pkg/apis/digitized.luxaslabs.com
API_DIRS := ${BOUNDING_API_DIRS}/v1alpha1
SRC_PKGS := cmd pkg
DOCKER_ARGS := --rm
CACHE_DIR := $(shell pwd)/bin/cache

# If we're not running in CI, run Docker interactively
ifndef CI
	DOCKER_ARGS += -it
endif

all: test

fmt:
	go fmt ./cmd/... ./pkg/...

vet:
	go vet ./cmd/... ./pkg/...

build:
	go build -o bin/vhs-converter ./cmd/vhs-converter
	go build -o bin/digitized ./cmd/digitized

test: build tidy fmt vet
	go test ./cmd/... ./pkg/... -coverprofile cover.out

docker-%:
	mkdir -p "${CACHE_DIR}/go" "${CACHE_DIR}/cache"
	docker run ${DOCKER_ARGS} \
		-u "${UID_GID}" \
		-v "${CACHE_DIR}/go":/go \
		-v "${CACHE_DIR}/cache":/.cache/go-build \
		-v "$(shell pwd):/go/src/${PROJECT}" \
		-w "/go/src/${PROJECT}" \
		"golang:${GO_VERSION}" make $*

tidy: docker-tidy-internal
tidy-internal: /go/bin/goimports
	go mod tidy
	# hack/generate-client.sh
	gofmt -s -w ${SRC_PKGS}
	goimports -w ${SRC_PKGS}

autogen: docker-autogen-internal
autogen-internal: /go/bin/deepcopy-gen /go/bin/defaulter-gen /go/bin/conversion-gen /go/bin/openapi-gen
	# Let the boilerplate be empty
	touch /tmp/boilerplate

	/go/bin/deepcopy-gen \
		--input-dirs ${API_DIRS},${PROJECT}/pkg/runtime \
		--bounding-dirs ${BOUNDING_API_DIRS} \
		-O zz_generated.deepcopy \
		-h /tmp/boilerplate

	/go/bin/defaulter-gen \
		--input-dirs ${API_DIRS} \
		-O zz_generated.defaults \
		-h /tmp/boilerplate

	/go/bin/conversion-gen \
		--input-dirs ${API_DIRS} \
		-O zz_generated.conversion \
		-h /tmp/boilerplate
	
	#/go/bin/openapi-gen \
	#	--input-dirs ${API_DIRS} \
	#	--output-package ${PROJECT}/api/openapi \
	#	--report-filename api/openapi/violations.txt \
	#	-h /tmp/boilerplate

	# These commands modify the environment, perform cleanup
	$(MAKE) tidy-internal

/go/bin/deepcopy-gen /go/bin/defaulter-gen /go/bin/conversion-gen: /go/bin/%:
	go get k8s.io/code-generator/cmd/$*

/go/bin/openapi-gen:
	go get k8s.io/kube-openapi/cmd/openapi-gen

/go/bin/goimports:
	go get golang.org/x/tools/cmd/goimports
