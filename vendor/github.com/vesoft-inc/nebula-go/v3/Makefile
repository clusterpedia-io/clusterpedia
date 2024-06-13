.PHONY: build unit test fmt up up-ssl down ssl-test run-examples

default: build

build: fmt
	go mod tidy
	go build
unit:
	go mod tidy
	go test -v -race --covermode=atomic --coverprofile coverage.out
test:
	go mod tidy
	go test -v -race --tags=integration --covermode=atomic --coverprofile coverage.out

fmt:
	go fmt
up:
	cd ./nebula-docker-compose && docker-compose up -d

up-ssl:
	cd ./nebula-docker-compose && enable_ssl=true docker-compose -f docker-compose-ssl.yaml up -d

down:
	cd ./nebula-docker-compose && docker-compose down -v

ssl-test:
	ssl_test=true go test -v --tags=integration -run TestSslConnection;

ssl-test-self-signed:
	self_signed=true go test -v --tags=integration -run TestSslConnection;

run-examples:
	go run basic_example/graph_client_basic_example.go && \
	go run basic_example/parameter_example.go && \
	go run gorountines_example/graph_client_goroutines_example.go && \
	go run json_example/parse_json_example.go && \
	go run session_pool_example/session_pool_example.go
