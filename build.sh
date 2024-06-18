#!/bin/bash
cd cce-network-v2

# 生产镜像发布
make docker PROFILE=pro PUSH_IMAGE_FLAGS=--push
make docker-arm GOARCH=arm64 PROFILE=pro PUSH_IMAGE_FLAGS=--push
