# 为集群更新容器网络 v2 组件的版本
# 使用示例 tools/operating/helm-upgrade.sh output/z85m1xdk/node-ready-values-z85m1xdk.yaml
#!/bin/bash

# 检查当前工作路径是否以 cce-network-v2 结尾
current_dir=$(basename "$PWD")
if [[ $current_dir != "cce-network-v2" ]]; then
  echo "错误：当前工作路径不是以 cce-network-v2 结尾。"
  exit 1
fi

if [ ! -f "$1" ]; then
  echo "错误：文件 '$1' 不存在。"
  exit 1
fi

resources=(
    # crd 资源
    "CustomResourceDefinition/cceendpoints.cce.baidubce.com"
    "CustomResourceDefinition/clusterpodsubnettopologyspreads.cce.baidubce.com"
    "CustomResourceDefinition/enis.cce.baidubce.com"
    "CustomResourceDefinition/netresourcesets.cce.baidubce.com"
    "CustomResourceDefinition/podsubnettopologyspreads.cce.baidubce.com"
    "CustomResourceDefinition/subnets.cce.baidubce.com"
    "CustomResourceDefinition/netresourceconfigsets.cce.baidubce.com"
    "CustomResourceDefinition/securitygroups.cce.baidubce.com"

    # 控制器资源
    "Deployment/cce-network-operator"
    "DaemonSet/cce-network-agent"
    "Service/cce-network-v2"

    # 配置文件
    "ConfigMap/cce-network-v2-config"
    "ConfigMap/cni-config-template"

    # 权限
    "ServiceAccount/cce-cni-v2"
    "ClusterRole/cce-cni-v2"
    "ClusterRoleBinding/cce-cni-v2"

    # webhook
    "MutatingWebhookConfiguration/cce-network-v2-mutating-webhook"
    # "ValidatingWebhookConfiguration/cce-network-v2-validating-webhook"
)

# 为每个资源执行增加 label 和 annotation 的操作
for resource in "${resources[@]}"; do
  kubectl -n kube-system label $resource app.kubernetes.io/managed-by=Helm --overwrite
  kubectl -n kube-system annotate $resource meta.helm.sh/release-namespace=kube-system meta.helm.sh/release-name=cce-network-v2 --overwrite
  echo "update $resource success"
done

# 由于 cce 组件安装时对 helm 解析的 bug，无法正确识别 helm 的标签，导致在升级过程中会出现以下错误
# Error: UPGRADE FAILED: failed to replace object: DaemonSet.apps "cce-network-agent" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"app.cce.baidubce.com":"cce-network-agent", "app.kubernetes.io/instance":"cce-network-v2", "app.kubernetes.io/name":"cce-network-v2"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable && failed to replace object: Deployment.apps "cce-network-operator" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"app.cce.baidubce.com":"cce-network-operator", "app.kubernetes.io/instance":"cce-network-v2", "app.kubernetes.io/name":"cce-network-v2"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable
# 所以在执行升级命令前，需要先删除掉原有的 ds 和 deployment
kubectl -n kube-system delete daemonset cce-network-agent 
kubectl -n kube-system delete deployment cce-network-operator

# 执行安装脚本
# 例如 tools/operating/helm-upgrade.sh output/z85m1xdk/node-ready-values-z85m1xdk.yaml
helm upgrade -n kube-system --install cce-network-v2 deploy/cce-network-v2 -f $1 --force