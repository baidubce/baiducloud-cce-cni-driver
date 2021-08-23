#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

ROOT_DIR=$(dirname "${BASH_SOURCE[0]}")/..
IMAGE_NAME="cce-cni-codegen:kubernetes-1.18.9"
SRC_DIR="/go/src/github.com/baidubce/baiducloud-cce-cni-driver"

function docker_run() {
  docker run --rm \
		-w ${SRC_DIR} \
		-v ${ROOT_DIR}:${SRC_DIR} \
		-v ~/.ssh/:/root/.ssh \
		"${IMAGE_NAME}" "$@"
}

docker_run hack/update-codegen-dockerized.sh
