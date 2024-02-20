.PHONY: build test test-dev fmt ci

default: build

build: fmt
	go mod tidy 
	go build

test: 
	go mod tidy 
	go test -v -race

fmt:
	go fmt

ci:
	cd ./nebula-docker-compose && docker-compose up -d && \
	sleep 5 && \
	cd .. && \
	go test -v -race; \
	cd ./nebula-docker-compose && docker-compose down -v 
