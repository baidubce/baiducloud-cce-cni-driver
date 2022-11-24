#!/bin/bash

set -e

CNI_CONF_DIR="/etc/cni/net.d"

# .conf/.conflist/.json (undocumented) are read by kubelet/dockershim's CNI implementation.
# Remove any active CCE CNI configurations to prevent scheduling Pods during agent
# downtime.
echo "Removing active CCE CNI configurations from ${CNI_CONF_DIR}..."
find "${CNI_CONF_DIR}" -maxdepth 1 -type f \
  -name '*cce-cni*' -and \( \
  -name '*.conf' -or \
  -name '*.conflist' \
  \) -delete

rm -rf /etc/cni/net.d/cce-cni.d