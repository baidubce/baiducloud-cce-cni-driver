module github.com/baidubce/baiducloud-cce-cni-driver

go 1.18

require (
	github.com/alexflint/go-filemutex v0.0.0-20171022225611-72bdc8eae2ae
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a
	github.com/baidubce/bce-sdk-go v0.9.117
	github.com/containernetworking/cni v0.8.0
	github.com/containernetworking/plugins v0.8.7
	github.com/davecgh/go-spew v1.1.1
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/mock v1.6.0
	github.com/golang/protobuf v1.5.2
	github.com/gorilla/mux v1.8.0
	github.com/im7mortal/kmutex v1.0.1
	github.com/j-keck/arping v1.0.1
	github.com/juju/ratelimit v1.0.1
	github.com/prometheus/client_golang v1.12.1
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.5
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/sys v0.6.0
	google.golang.org/grpc v1.36.1
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.24.2
	k8s.io/apimachinery v0.24.2
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/component-base v0.24.2
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.4.0
	k8s.io/kubectl v0.0.0
	k8s.io/kubernetes v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	modernc.org/mathutil v1.0.0
	sigs.k8s.io/controller-runtime v0.6.5
)

require (
	github.com/cilium/ebpf v0.7.0 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/cyphar/filepath-securejoin v0.2.3 // indirect
	github.com/godbus/dbus/v5 v5.0.6 // indirect
	github.com/moby/sys/mountinfo v0.5.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/coreos/go-iptables v0.4.5 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/spdystream v0.0.0-20160310174837-449fdfce4d96 // indirect
	github.com/go-logr/logr v0.2.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/googleapis/gnostic v0.4.1 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/imdario/mergo v0.3.9 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runc v1.1.6
	github.com/opencontainers/selinux v1.10.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20200410134404-eec4a21b6bb0 // indirect
	github.com/safchain/ethtool v0.0.0-20190326074333-42ed695e3de8 // indirect
	github.com/vishvananda/netns v0.0.0-20200728191858-db3c7e526aae // indirect
	go.uber.org/atomic v1.4.0 // indirect
	go.uber.org/multierr v1.1.0 // indirect
	go.uber.org/zap v1.10.0 // indirect
	golang.org/x/crypto v0.0.0-20201002170205-7f63de1d35b0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/oauth2 v0.0.0-20210514164344-f6687ab2804c // indirect
	golang.org/x/text v0.8.0 // indirect
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e // indirect
	gomodules.xyz/jsonpatch/v2 v2.0.1 // indirect
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/genproto v0.0.0-20201110150050-8816d57aaa9a // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.20.9 // indirect
	k8s.io/apiserver v0.20.15 // indirect
	k8s.io/cloud-provider v0.20.15 // indirect
	k8s.io/controller-manager v0.20.15 // indirect
	k8s.io/cri-api v0.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20211110013926-83f114cd0513 // indirect
	k8s.io/mount-utils v0.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.2 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace (
	github.com/go-logr/logr => github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr => github.com/go-logr/zapr v0.4.0
	// github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.3.1
	github.com/im7mortal/kmutex => github.com/im7mortal/kmutex v1.0.2-0.20211009180904-795f0d162683
	google.golang.org/grpc v1.30.0 => google.golang.org/grpc v1.29.1
	k8s.io/api => k8s.io/api v0.20.15
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.20.15
	k8s.io/apimachinery => k8s.io/apimachinery v0.20.15
	k8s.io/apiserver => k8s.io/apiserver v0.20.15
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.20.15
	k8s.io/client-go => k8s.io/client-go v0.20.15
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.20.15
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.20.15
	k8s.io/code-generator => k8s.io/code-generator v0.20.15
	k8s.io/component-base => k8s.io/component-base v0.20.15
	k8s.io/component-helpers => k8s.io/component-helpers v0.20.15
	k8s.io/controller-manager => k8s.io/controller-manager v0.20.15
	k8s.io/cri-api => k8s.io/cri-api v0.20.15
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.20.15
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.20.15
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.20.15
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.20.15
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.20.15
	k8s.io/kubectl => k8s.io/kubectl v0.20.15
	k8s.io/kubelet => k8s.io/kubelet v0.20.15
	k8s.io/kubernetes => k8s.io/kubernetes v1.20.15
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.20.15
	k8s.io/metrics => k8s.io/metrics v0.20.15
	k8s.io/mount-utils => k8s.io/mount-utils v0.20.15
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.20.15
)
