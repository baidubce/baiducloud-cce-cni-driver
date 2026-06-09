FROM registry.baidubce.com/cce-plugin-dev/golang:1.23.6-tidy

LABEL maintainer="ZhongZhaowei<zhongzhaowei@baidu.com>"

ARG WORKDIR=cce-network-v2
WORKDIR /go/src

# Copy both bce-sdk-go and cce-network-v2
COPY bce-sdk-go ./bce-sdk-go
COPY ${WORKDIR} ./cce-network-v2

WORKDIR /go/src/cce-network-v2

# Remove the conflicting centos.go file if it exists
RUN rm -f pkg/os/centos.go

RUN go mod tidy