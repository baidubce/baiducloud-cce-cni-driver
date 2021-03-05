# init project path
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output

# init command params
GO      := $(GO_1_16_BIN)/go
GOROOT  := $(GO_1_16_HOME)
GOPATH  := $(shell $(GO) env GOPATH)
GOMOD   := $(GO) mod
GOBUILD := CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build
GOTEST  := $(GO) test -gcflags="-N -l"
GOPKGS  := $$($(GO) list ./...| grep -vE "vendor")
GOGCFLAGS := -gcflags=all="-trimpath=$(GOPATH)" -asmflags=all="-trimpath=$(GOPATH)"
GOLDFLAGS := -ldflags '-s -w'
GO_PACKAGE := github.com/baidubce/baiducloud-cce-cni-driver
# test cover files
COVPROF := $(HOMEDIR)/covprof.out  # coverage profile
COVFUNC := $(HOMEDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(HOMEDIR)/covhtml.html # HTML representation of coverage profile

# versions
VERSION := v1.2.1
FELIX_VERSION := v3.5.8
K8S_VERSION := 1.16.8

#
GIT_COMMIT := $(shell git rev-parse HEAD)
Git_SUMMARY := $(shell git describe --tags --dirty --always)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')


EXTRALDFLAGS := -X $(GO_PACKAGE)/pkg/version.GitCommit=$(GIT_COMMIT)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.GitSummary=$(Git_SUMMARY)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.BuildDate=$(BUILD_DATE)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.Version=$(VERSION)

# make, make all
all: prepare compile package

# set proxy env
set-env:
	$(GO) env -w GO111MODULE=on
	$(GO) env -w GONOPROXY=\*\*.baidu.com\*\*
	$(GO) env -w GOPROXY=https://goproxy.baidu-int.com
	$(GO) env -w GONOSUMDB=\*

#make prepare, download dependencies
prepare: gomod

gomod: set-env
	$(GOMOD) download

#make compile
compile: build

build:
	@echo "===> Building cni components <==="
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/eni-ipam $(HOMEDIR)/cni/eni-ipam
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/ipvlan $(HOMEDIR)/cni/ipvlan
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/ptp $(HOMEDIR)/cni/ptp
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/sysctl $(HOMEDIR)/cni/sysctl
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/unnumbered-ptp $(HOMEDIR)/cni/unnumbered-ptp
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -ldflags '$(EXTRALDFLAGS)' -o $(HOMEDIR)/cce-ipam $(HOMEDIR)/cmd/eni-ipam
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -ldflags '$(EXTRALDFLAGS)' -o $(HOMEDIR)/cni-node-agent $(HOMEDIR)/cmd/node-agent

# make test, test your code
test: prepare test-case
test-case:
	$(GOTEST) -v -cover $(GOPKGS)

# make package
package: package-bin
package-bin:
	@echo "===> Packing cni components <==="
	mkdir -p $(OUTDIR)/cni-bin
	# package cni binaries
	mv $(HOMEDIR)/eni-ipam $(OUTDIR)/cni-bin/
	mv $(HOMEDIR)/ipvlan $(OUTDIR)/cni-bin/
	mv $(HOMEDIR)/ptp $(OUTDIR)/cni-bin/
	mv $(HOMEDIR)/sysctl $(OUTDIR)/cni-bin/
	mv $(HOMEDIR)/unnumbered-ptp $(OUTDIR)/cni-bin/
	# package components
	mv $(HOMEDIR)/cce-ipam $(OUTDIR)
	mv $(HOMEDIR)/cni-node-agent $(OUTDIR)

debian-iptables-image:
	@echo "===> Building debian iptables base image <==="
	docker build -t cce-cni-debian-iptables:v1.0.0 -f build/images/debian-iptables/Dockerfile build/images/debian-iptables

codegen-image:
	@echo "===> Building codegen image <==="
	docker build -t cce-cni-codegen:kubernetes-$(K8S_VERSION) -f build/images/codegen/Dockerfile build/images/codegen

cni-image: package-bin debian-iptables-image
	@echo "===> Building cce cni image <==="
	docker build -t registry.baidubce.com/cce-plugin-pro/cce-cni:$(VERSION) -f build/images/cce-cni/Dockerfile .

felix-image:
	@echo "===> Building cce felix image <==="
	docker build -t registry.baidubce.com/cce-plugin-pro/cce-calico-felix:$(FELIX_VERSION) -f build/images/cce-felix/Dockerfile pkg/policy

push-cni-image:cni-image
	@echo "===> Pushing cce cni image <==="
	docker push registry.baidubce.com/cce-plugin-pro/cce-cni:$(VERSION)

push-felix-image:felix-image
	@echo "===> Pushing cce felix image <==="
	docker push registry.baidubce.com/cce-plugin-pro/cce-calico-felix:$(FELIX_VERSION)

push-cni-test-image: build package-bin debian-iptables-image
	@echo "===> Building cce cni test image <==="
	docker build -t registry.baidubce.com/cce-plugin-dev/cce-cni:$(TAG) -f build/images/cce-cni/Dockerfile .
	@echo "===> Pushing cce cni test image <==="
	docker push registry.baidubce.com/cce-plugin-dev/cce-cni:$(TAG)

codegen:codegen-image
	@echo "===> Updating generated code <==="
	$(HOMEDIR)/hack/update-codegen.sh

charts:
	@helm template build/yamls/cce-cni-driver -f $(VALUES)

# make clean
clean:
	$(GO) clean
	rm -rf $(OUTDIR)
	rm -rf $(GOPATH)/pkg/darwin_amd64

# avoid filename conflict and speed up build 
.PHONY: all prepare compile test package clean build
