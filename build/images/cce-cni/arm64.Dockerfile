FROM registry.baidubce.com/cce-plugin-pro/cce-cni-base:v1.0.0-arm64

LABEL maintainer="Chen Yaqi<chenyaqi01@baidu.com>"

# install cni binaries
COPY output/cni-bin/unnumbered-ptp /unnumbered-ptp
COPY output/cni-bin/ipvlan /ipvlan
COPY output/cni-bin/macvlan /macvlan
COPY output/cni-bin/bandwidth /bandwidth
COPY output/cni-bin/ptp /ptp
COPY output/cni-bin/eni-ipam /eni-ipam
COPY output/cni-bin/sysctl /sysctl
COPY output/cni-bin/crossvpc-eni /crossvpc-eni
COPY output/cni-bin/roce /roce

# install cce ipam binary
COPY output/cce-ipam /bin/cce-ipam

# install cce node agent binary
COPY output/cni-node-agent /bin/cni-node-agent

# install cce node agent binary
COPY output/ip-masq-agent /bin/cce-ip-masq-agent

CMD ["/bin/bash", "/entrypoint.sh"]
