GIT_SHA := $(shell git rev-parse --short HEAD)
SRC := $(shell find . -name \*.go)
GO_LINUX := GOOS=linux GOARCH=amd64 go

.PHONY: all
all: docker

.PHONY: coverage
coverage: coverage.out

# run the tests with atomic coverage
cover.out: $(SRC)
	go test -v -cover -covermode atomic -coverprofile cover.out ./...

# generate the HTML coverage report
coverage.html: cover.out
	go tool cover -html=cover.out -o coverage.html

# generate the text coverage summary
coverage.out: cover.out
	go tool cover -func=cover.out | tee coverage.out

majortom.amd64: $(SRC)
	$(GO_LINUX) build -v -ldflags "-X main.Revision=$(GIT_SHA)" -o $@

.PHONY: docker
docker: majortom.amd64 cover.out
	docker build . -t nfinstana/majortom:latest -t nfinstana/majortom:$(GIT_SHA)

.PHONY: dev
dev: docker
	docker push nfinstana/majortom:$(GIT_SHA)

.PHONY: publish
publish: dev
	docker push nfinstana/majortom:latest
