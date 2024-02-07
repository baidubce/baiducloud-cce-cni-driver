# init project path
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output

# init command params
export GO := $(GO_1_20_BIN)/go
ifeq ($(GO), /go)
	GO = go
endif
export GOROOT  := $(GO_1_20_HOME)
GOPATH  := $(shell $(GO) env GOPATH)
GOPKGS  := $$($(GO) list ./...| grep "pkg" |grep -v "vendor" | grep -v "cmd" |grep -v "test" | grep -v 'api' |grep -v "generated" | grep -v 'pkg/bce' | grep -v config | grep -v metric | grep -v rpc | grep -v version | grep -v wrapper | grep -v util)
GOGCFLAGS := -gcflags=all="-trimpath=$(GOPATH)" -asmflags=all="-trimpath=$(GOPATH)"
GOLDFLAGS := -ldflags '-s -w'
GOMOD   := $(GO) mod
GOBUILD = CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) build
GOTEST  := $(GO) test -race -timeout 30s -gcflags="-N -l"
GOPKGS  := $$($(GO) list ./...| grep -vE "vendor")

# test cover files
COVPROF := $(HOMEDIR)/covprof.out  # coverage profile
COVFUNC := $(HOMEDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(HOMEDIR)/covhtml.html # HTML representation of coverage profile

SUBDIRS = cce-network-v2 eip-operator


# make, make all
all: prepare

# set proxy env
set-env:
	$(GO) env -w GO111MODULE=on
	$(GO) env -w GONOPROXY=\*.baidu.com\*
	$(GO) env -w GOPROXY=https://goproxy.baidu-int.com
	$(GO) env -w GONOSUMDB=\*
	$(GO) env -w CC=/opt/compiler/gcc-8.2/bin/gcc
	$(GO) env -w CXX=/opt/compiler/gcc-8.2/bin/g++
	$(GO) work sync

#make prepare, download dependencies
prepare: set-env
	mkdir -p $(OUTDIR)
	go env

gomod: set-env
	$(GOMOD) download -x || $(GOMOD) download -x

#make compile
compile: build

build: prepare
	make -C cce-network-v2 build
	mv cce-network-v2/output $(OUTDIR)/cce-network-v2

# make test, test your code
test: prepare test-case
test-case:
	$(GOTEST) -v -cover $(GOPKGS)

# make package
package: package-bin
package-bin:
	mkdir -p $(OUTDIR)
	mv cce-network-plugin  $(OUTDIR)/

# make clean
clean:
	$(GO) clean
	rm -rf $(OUTDIR)
	rm -rf $(HOMEDIR)/cce-network-plugin
	rm -rf $(GOPATH)/pkg/darwin_amd64

# avoid filename conflict and speed up build 
.PHONY: all prepare compile test package clean build