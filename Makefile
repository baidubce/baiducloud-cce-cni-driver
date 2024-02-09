# init project path
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output

# init command params
GO      := $(GO_1_18_BIN)/go
ifeq ($(GO), /go)
	GO = go
endif
GOROOT  := $(GO_1_18_HOME)
GOPATH  := $(shell $(GO) env GOPATH)
GOMOD   := $(GO) mod
GOARCH  := $(shell $(GO) env GOARCH)
GOBUILD = CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) build
GOTEST  := $(GO) test
GOPKGS  := $$($(GO) list ./...| grep "pkg" |grep -v "vendor" | grep -v "cmd" |grep -v "test" | grep -v 'api' |grep -v "generated" | grep -v 'pkg/bce' | grep -v config | grep -v metric | grep -v rpc | grep -v version | grep -v wrapper | grep -v util)
GOGCFLAGS := -gcflags=all="-trimpath=$(GOPATH)" -asmflags=all="-trimpath=$(GOPATH)"
GOLDFLAGS := -ldflags '-s -w'
GO_PACKAGE := github.com/baidubce/baiducloud-cce-cni-driver
# test cover files
COVPROF := $(HOMEDIR)/covprof.out  # coverage profile
COVFUNC := $(HOMEDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(HOMEDIR)/covhtml.html # HTML representation of coverage profile

# versions
VERSION := v1.8.7
FELIX_VERSION := v3.5.8
K8S_VERSION := 1.18.9

# build info
GIT_COMMIT := $(shell git rev-parse HEAD)
GIT_SUMMARY := $(shell git describe --tags --dirty --always)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')


EXTRALDFLAGS := -X $(GO_PACKAGE)/pkg/version.GitCommit=$(GIT_COMMIT)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.GitSummary=$(GIT_SUMMARY)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.BuildDate=$(BUILD_DATE)
EXTRALDFLAGS += -X $(GO_PACKAGE)/pkg/version.Version=$(VERSION)

# pro or dev
PROFILES := dev
IMAGE_TAG := registry.baidubce.com/cce-plugin-$(PROFILES)/cce-cni
PUSH_CNI_IMAGE_FLAGS = --load --push

# make, make all
all: prepare compile

fmt:  ## Run go fmt against code.
	$(GO) fmt $(shell $(GO) list ./... | grep -v /vendor/)

vet: ## Run go vet against code.
	$(GO) vet $(shell $(GO) list ./... | grep -v /vendor/)

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

outdir: 
	mkdir -p $(OUTDIR)/cni-bin
# Compile all cni plug-ins
cni_target := eni-ipam ipvlan macvlan bandwidth ptp sysctl unnumbered-ptp crossvpc-eni rdma eri
$(cni_target): fmt outdir
	@echo "===> Building cni $@ <==="
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -o $(HOMEDIR)/$@ $(HOMEDIR)/cni/$@
	mv $(HOMEDIR)/$@ $(OUTDIR)/cni-bin/

# Compile all container network programs
exec_target := cce-ipam cni-node-agent ip-masq-agent	
$(exec_target):	fmt outdir
	@echo "===> Building cni $@ <==="
	$(GOBUILD) $(GOLDFLAGS) $(GOGCFLAGS) -ldflags '$(EXTRALDFLAGS)' -o $(HOMEDIR)/$@ $(HOMEDIR)/cmd/$@
	mv $(HOMEDIR)/$@ $(OUTDIR)

#make compile
compile: $(cni_target) $(exec_target)
build: compile

# make test, test your code
test: prepare test-case
test-case:
	$(GOTEST) -v -cover -parallel 16 $(GOPKGS)

debian-iptables-image:
	@echo "===> Building debian iptables base image <==="
	docker build -t cce-cni-debian-iptables:v1.0.0 -f build/images/debian-iptables/Dockerfile build/images/debian-iptables

codegen-image:
	@echo "===> Building codegen image <==="
	docker build -t cce-cni-codegen:kubernetes-$(K8S_VERSION) -f build/images/codegen/Dockerfile build/images/codegen

cni-amd64-image: GOARCH = amd64
cni-amd64-image: compile
	docker buildx build --platform linux/amd64  -t $(IMAGE_TAG):$(VERSION) -f build/images/cce-cni/Dockerfile . $(PUSH_CNI_IMAGE_FLAGS)

cni-arm64-image: GOARCH = arm64
cni-arm64-image: compile
	# docker buildx create --name arm
	docker buildx use arm && docker buildx build --platform linux/arm64  -t $(IMAGE_TAG)-arm64:$(VERSION) -f build/images/cce-cni/arm64.Dockerfile . $(PUSH_CNI_IMAGE_FLAGS)

image: cni-amd64-image

felix-image:
	@echo "===> Building cce felix image <==="
	docker build -t registry.baidubce.com/cce-plugin-pro/cce-calico-felix:$(FELIX_VERSION) -f build/images/cce-felix/Dockerfile pkg/policy

push-felix-image:felix-image
	@echo "===> Pushing cce felix image <==="
	docker push registry.baidubce.com/cce-plugin-pro/cce-calico-felix:$(FELIX_VERSION)

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
