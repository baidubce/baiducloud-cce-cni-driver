FROM registry.baidubce.com/cce-plugin-dev/golang:1.23.6-tidy

LABEL maintainer="ZhongZhaowei<zhongzhaowei@baidu.com>"

ARG WORKDIR=cce-network-v2
WORKDIR /go/src

# Copy both bce-sdk-go and cce-network-v2
COPY bce-sdk-go ./bce-sdk-go
COPY ${WORKDIR} ./cce-network-v2

WORKDIR /go/src/cce-network-v2

# Set GOPROXY to use multiple proxies for better reliability
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org

RUN go mod tidy