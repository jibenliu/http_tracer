PROJECT="userHttpTrace"
VERSION=1.0.0
BUILD=`date +%FT%T%z`

default:
	echo ${PROJECT}
	@go build -o ${BINARY} -tags=jsoniter

install:
	go mod tidy
	go mod vendor

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build main.go

before_build:
	make install
test:
	@go test ./...

.PHONY: default install test build