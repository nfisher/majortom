GIT_SHA := $(shell git rev-parse --short HEAD)
SRC := $(shell find . -name \*.go)

.PHONY: all
all: docker

majortom.amd64: $(SRC)
	GOOS=linux GOARCH=amd64 go build -v -v -ldflags "-X main.Revision=$(GIT_SHA)" -o $@

.PHONY: docker
docker: majortom.amd64
	docker build . -t nfinstana/majortom:latest -t nfinstana/majortom:$(GIT_SHA)

.PHONY: publish
publish: docker
	docker push nfinstana/majortom:$(GIT_SHA)
	docker push nfinstana/majortom:latest