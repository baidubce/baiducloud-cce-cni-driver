#!/bin/bash
cd cce-network-v2

# 生产镜像发布
export EXTRA_GO_BUILD_FLAGS=-gcflags=-trimpath=$GOPATH/src
make docker PROFILE=pro PUSH_IMAGE_FLAGS=--push
make docker-arm GOARCH=arm64 PROFILE=pro PUSH_IMAGE_FLAGS=--push
