FROM cce-cni-debian-iptables:v1.0.0

LABEL maintainer="Chen Yaqi<chenyaqi01@baidu.com>"

# ensure cni bin from https://github.com/containernetworking/plugins/releases/download/v0.8.5/cni-plugins-linux-amd64-v0.8.5.tgz
RUN curl -L -k --retry 5 https://github.com/containernetworking/plugins/releases/download/v0.8.5/cni-plugins-linux-amd64-v0.8.5.tgz | tar -xz -C / ./loopback ./host-local ./portmap

# install entrypoint.sh
COPY build/images/cce-cni/entrypoint.sh /entrypoint.sh

# install cni binaries
COPY output/cni-bin/unnumbered-ptp /unnumbered-ptp
COPY output/cni-bin/ipvlan /ipvlan
COPY output/cni-bin/macvlan /macvlan
COPY output/cni-bin/bandwidth /bandwidth
COPY output/cni-bin/ptp /ptp
COPY output/cni-bin/eni-ipam /eni-ipam
COPY output/cni-bin/sysctl /sysctl

# install cce ipam binary
COPY output/cce-ipam /bin/cce-ipam

# install cce node agent binary
COPY output/cni-node-agent /bin/cni-node-agent

# install cce node agent binary
COPY output/ip-masq-agent /bin/cce-ip-masq-agent

CMD ["/bin/bash", "/entrypoint.sh"]