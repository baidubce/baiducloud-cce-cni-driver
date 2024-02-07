FROM registry.baidubce.com/cce-plugin-dev/ubuntu-net-22-04:1.1

LABEL maintainer="WeiweiWang<wangweiwei22@baidu.com>"
WORKDIR /home/cce




# install cce node agent binary
COPY output/bin/cmd/agent /bin/agent

# install cni binaries
COPY output/bin/cmd/pcb-ipam /cni/pcb-ipam
COPY deploy/dockerfile/install-cni.sh install-cni.sh
# 其它全量cni插件
#COPY output/bin/cni/* /cni/
#COPY output/test/config/10-macvlan-ipam.conflist /home/cce/net.d/

CMD ["agent"]