module github.com/baidubce/baiducloud-cce-cni-driver

go 1.16

require (
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a
	github.com/baidubce/bce-sdk-go v0.9.117
	github.com/containernetworking/cni v0.8.0
	github.com/containernetworking/plugins v0.8.7
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/golang/mock v1.4.4
	github.com/golang/protobuf v1.4.2
	github.com/gorilla/mux v1.8.0
	github.com/im7mortal/kmutex v1.0.1
	github.com/j-keck/arping v1.0.1
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/juju/ratelimit v1.0.1
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.0.0
	github.com/remyoudompheng/bigfft v0.0.0-20200410134404-eec4a21b6bb0 // indirect
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/sys v0.0.0-20200728102440-3e129f6d46b1
	golang.org/x/text v0.3.3 // indirect
	google.golang.org/grpc v1.30.0
	gopkg.in/yaml.v2 v2.3.0 // indirect
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/component-base v0.19.0
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	modernc.org/mathutil v1.0.0
)

replace (
	github.com/im7mortal/kmutex => github.com/im7mortal/kmutex v1.0.2-0.20211009180904-795f0d162683
	google.golang.org/grpc v1.30.0 => google.golang.org/grpc v1.29.1
	k8s.io/api => k8s.io/api v0.18.9
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.9
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.9
	k8s.io/apiserver => k8s.io/apiserver v0.18.9
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.9
	k8s.io/client-go => k8s.io/client-go v0.18.9
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.18.9
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.18.9
	k8s.io/code-generator => k8s.io/code-generator v0.18.9
	k8s.io/component-base => k8s.io/component-base v0.18.9
	k8s.io/cri-api => k8s.io/cri-api v0.18.9
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.18.9
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.18.9
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.18.9
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.18.9
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.18.9
	k8s.io/kubectl => k8s.io/kubectl v0.18.9
	k8s.io/kubelet => k8s.io/kubelet v0.18.9
	k8s.io/kubernetes => k8s.io/kubernetes v1.18.9
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.18.9
	k8s.io/metrics => k8s.io/metrics v0.18.9
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.18.9
)
