FROM ubuntu:18.04 as protoc

RUN apt-get update && \
    apt-get install -y --no-install-recommends wget ca-certificates unzip

RUN PROTOBUF_VERSION=3.13.0; ZIPNAME="protoc-${PROTOBUF_VERSION}-linux-x86_64.zip"; \
  mkdir /tmp/protoc && cd /tmp/protoc && \
  wget "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOBUF_VERSION}/${ZIPNAME}" && \
  unzip "${ZIPNAME}" && \
  chmod -R +rX /tmp/protoc


FROM golang:1.16

LABEL description="A Docker image based on golang 1.16 which includes codegen tools."


ENV GO111MODULE=on

# Set baidu golang env
RUN go env -w GO111MODULE="on" && \
    go env -w GONOPROXY=\*\*.baidu.com\*\*  && \
    go env -w GONOSUMDB=\* && \
    go env -w GOPROXY=https://goproxy.baidu-int.com && \
    go env -w GOPRIVATE=\*.baidu.com && \
    go env -w GOFLAGS="-mod=mod"

ARG K8S_VERSION=1.18.9

RUN go get k8s.io/code-generator/cmd/client-gen@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/deepcopy-gen@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/conversion-gen@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/lister-gen@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/informer-gen@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/go-to-protobuf@kubernetes-$K8S_VERSION && \
    go get k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo@kubernetes-$K8S_VERSION && \
    go get github.com/golang/mock/mockgen@v1.5.0 && \
    go get github.com/golang/protobuf/protoc-gen-go@v1.3.2 && \
    go get golang.org/x/tools/cmd/goimports

COPY --from=protoc /tmp/protoc/bin /usr/local/bin
COPY --from=protoc /tmp/protoc/include /usr/local/include

