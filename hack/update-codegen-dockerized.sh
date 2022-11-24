#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -o xtrace

GOPATH=`go env GOPATH`

CNI_PKG="github.com/baidubce/baiducloud-cce-cni-driver"

# Generate protobuf code for CNI gRPC service with protoc.
protoc pkg/rpc/rpc.proto --go_out=plugins=grpc:.

# Generate deepcopy code with K8s codegen tools.
$GOPATH/bin/deepcopy-gen \
  --input-dirs "${CNI_PKG}/pkg/apis/networking/v1alpha1" \
  -O zz_generated.deepcopy \
  --go-header-file hack/boilerplate.go.txt

# Generate clientset and apis code with K8s codegen tools.
$GOPATH/bin/client-gen \
  --clientset-name versioned \
  --input-base "" \
  --input "${CNI_PKG}/pkg/apis/networking/v1alpha1" \
  --output-package "${CNI_PKG}/pkg/generated/clientset" \
  --go-header-file hack/boilerplate.go.txt

# Generate listers with K8s codegen tools.
$GOPATH/bin/lister-gen \
  --input-dirs "${CNI_PKG}/pkg/apis/networking/v1alpha1" \
  --output-package "${CNI_PKG}/pkg/generated/listers" \
  --go-header-file hack/boilerplate.go.txt

# Generate informers with K8s codegen tools.
$GOPATH/bin/informer-gen \
  --input-dirs "${CNI_PKG}/pkg/apis/networking/v1alpha1" \
  --versioned-clientset-package "${CNI_PKG}/pkg/generated/clientset/versioned" \
  --listers-package "${CNI_PKG}/pkg/generated/listers" \
  --output-package "${CNI_PKG}/pkg/generated/informers" \
  --go-header-file hack/boilerplate.go.txt


# Generate mocks for testing with mockgen.
MOCKGEN_TARGETS=(
  "pkg/bce/cloud Interface"
  "pkg/bce/metadata Interface"
  "pkg/eniipam/ipam Interface,ExclusiveEniInterface"
  "pkg/util/network Interface"
  "pkg/util/fs FileSystem"
  "pkg/util/kernel Interface"
  "pkg/wrapper/cnitypes Interface"
  "pkg/wrapper/grpc Interface"
  "pkg/wrapper/ip Interface"
  "pkg/wrapper/ipam Interface"
  "pkg/wrapper/netlink Interface"
  "pkg/wrapper/netns NetNS"
  "pkg/wrapper/ns Interface"
  "pkg/wrapper/rpc Interface"
  "pkg/wrapper/sysctl Interface"
  "pkg/rpc CNIBackendClient"
)

# Command mockgen does not automatically replace variable YEAR with current year
# like others do, e.g. client-gen.
current_year=$(date +"%Y")
sed -i "s/YEAR/${current_year}/g" hack/boilerplate.go.txt
for target in "${MOCKGEN_TARGETS[@]}"; do
  read -r package interfaces <<<"${target}"
  package_name=$(basename "${package}")
  $GOPATH/bin/mockgen \
    -copyright_file hack/boilerplate.go.txt \
    -destination "${package}/testing/mock_${package_name}.go" \
    -package=testing \
    "${CNI_PKG}/${package}" "${interfaces}"
done