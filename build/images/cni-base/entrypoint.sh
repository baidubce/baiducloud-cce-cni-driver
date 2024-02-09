#!/bin/bash
set -u -e

CNI_BINARY_DIR=/opt/cni/bin/
CNI_PLUGIN_LIST="bridge unnumbered-ptp ipvlan macvlan bandwidth loopback host-local ptp eni-ipam sysctl portmap crossvpc-eni"

# mv cni binary to dest
for PLUGIN in $CNI_PLUGIN_LIST
do
  mv /$PLUGIN $CNI_BINARY_DIR
done

CNI_CONF_DIR="/etc/cni/net.d"
CNI_CONF_NAME="00-cce-cni.conflist"
CCE_CNI_KUBECONFIG=$CNI_CONF_DIR/cce-cni.d/cce-cni.kubeconfig
mkdir -p $CNI_CONF_DIR/cce-cni.d

# rename all remaining cni configurations to *.cce_cni_bak
find $CNI_CONF_DIR \
     -maxdepth 1 \
     -type f \
     \( -name '*.conf' \
     -or -name '*.conflist' \
     -or -name '*.json' \
     \) \
     -not \( -name '*.cce_cni_bak' \
     -or -name $CNI_CONF_NAME \) \
     -exec mv {} {}.cce_cni_bak \;


# Generate a "kube-config"
SERVICE_ACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount
KUBE_CA_FILE=${KUBE_CA_FILE:-$SERVICE_ACCOUNT_PATH/ca.crt}
SERVICEACCOUNT_TOKEN=$(cat $SERVICE_ACCOUNT_PATH/token)


# Check if we're running as a k8s pod.
if [ -f "$SERVICE_ACCOUNT_PATH/token" ]; then
  # We're running as a k8d pod - expect some variables.
  if [ -z ${KUBERNETES_SERVICE_HOST} ]; then
    error "KUBERNETES_SERVICE_HOST not set"; exit 1;
  fi
  if [ -z ${KUBERNETES_SERVICE_PORT} ]; then
    error "KUBERNETES_SERVICE_PORT not set"; exit 1;
  fi
  TLS_CFG="certificate-authority-data: $(cat $KUBE_CA_FILE | base64 | tr -d '\n')"
  # Write a kubeconfig file for the CNI plugin.  Do this
  # to skip TLS verification for now.  We should eventually support
  # writing more complete kubeconfig files. This is only used
  # if the provided CNI network config references it.
  touch $CCE_CNI_KUBECONFIG
  chmod ${KUBECONFIG_MODE:-600} $CCE_CNI_KUBECONFIG
  cat > $CCE_CNI_KUBECONFIG <<EOF
# Kubeconfig file for CCE CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://[${KUBERNETES_SERVICE_HOST}]:${KUBERNETES_SERVICE_PORT}
    $TLS_CFG
users:
- name: cce-cni
  user:
    token: "${SERVICEACCOUNT_TOKEN}"
contexts:
- name: cce-cni-context
  context:
    cluster: local
    user: cce-cni
current-context: cce-cni-context
EOF
else
  warn "Doesn't look like we're running in a kubernetes environment (no serviceaccount token)"
fi
