VERSION=0.0.1
BUILD_TIME=`date +%FT%T%z`
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

build: kbox

libs/src/kbox/arrangements.pb.go: libs/src/kbox/arrangements.proto
	protoc --go_out=plugins=grpc,import_path=kbox:. libs/src/kbox/arrangements.proto

kbox: kbox.go libs/src/kbox/arrangements.pb.go
	go build ${LDFLAGS} kbox.go

install:
	go install

clean:
	go clean

run:
	 @$(MAKE) --no-print-directory && ${TM_PROJECT_DIRECTORY}/kbox

