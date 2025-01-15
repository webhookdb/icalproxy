ifdef DEBUG
DEBUGARG := --log-level=debug
endif

XTRAARGS := ${DEBUGARG}

BIN := ./icalproxy

install:
	go install github.com/onsi/ginkgo/v2/ginkgo

up:
	@docker compose up -d
stop:
	@docker compose stop

fmt:
	go fmt ./...

test:
	LOG_LEVEL=error ginkgo ./... --race

test-watch:
	LOG_LEVEL=error ginkgo watch ./...

build:
	@go build -ldflags \
		"-X github.com/webhookdb/icalproxy/config.BuildTime=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
		-X github.com/webhookdb/icalproxy/config.BuildSha=`git rev-list -1 HEAD`"

vet:
	go vet ./...

lint:
	gofmt -l .

check: lint vet test

update-lithic-deps:
	go get github.com/rgalanakis/golangal@latest
	go get github.com/lithictech/go-aperitif@latest

psql:
	pgcli "postgres://ical:ical@127.0.0.1:9025/ical"
psql-test:
	pgcli postgres://ical:ical@127.0.0.1:9026/test_ical

help: build
	@${BIN}

server: build
	${BIN} server \
		${XTRAARGS}
