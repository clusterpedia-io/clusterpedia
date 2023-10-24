all: test

vendor:
	go mod vendor

lint:
	$(ENV) golangci-lint run -c .golangci.yml --fix ./...

test:
	go test -coverprofile=coverage.out ./...

mock:
	# go get github.com/golang/mock/gomock
	# Source Mode
	@mockgen -package=mocks -destination dialectors/mocks/dialector.go -source=dialectors/dialector.go

.PHONY: test
