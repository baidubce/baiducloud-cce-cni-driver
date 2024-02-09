FROM cce-cni-debian-iptables:v1.0.0

LABEL maintainer="Chen Yaqi<chenyaqi01@baidu.com>"

# ensure cni bin from https://github.com/containernetworking/plugins/releases/download/v1.0.1/cni-plugins-linux-${arch}-v1.0.1.tgz
RUN arch=$(arch | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) && \
  curl -L -k --retry 5 https://github.com/containernetworking/plugins/releases/download/v1.0.1/cni-plugins-linux-${arch}-v1.0.1.tgz | tar -xz -C / ./loopback ./host-local ./portmap ./bridge

# install entrypoint.sh and uninstall-cni.sh
COPY build/images/cce-cni/entrypoint.sh /entrypoint.sh
COPY build/images/cce-cni/uninstall-cni.sh /uninstall-cni.sh

CMD ["/bin/bash", "/entrypoint.sh"]
