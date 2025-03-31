FROM registry.baidubce.com/cce-plugin-dev/ubuntu-net-22-04:1.1

LABEL maintainer="XuezhenMa<maxuezhen@baidu.com>"
WORKDIR /home/cce

# install cce node agent binary
COPY output/bin/cmd/exclusive-rdma-agent /bin/exclusive-rdma-agent

# install cni binaries
COPY output/bin/plugins/exclusive-rdma /cni/exclusive-rdma
COPY cmd/exclusive-rdma-agent/install-exclusive-rdma.sh install-exclusive-rdma.sh

# simply used to complete dockerfile, command is overwritten by the daemonset and will not be executed
CMD ["/bin/exclusive-rdma-agent"]