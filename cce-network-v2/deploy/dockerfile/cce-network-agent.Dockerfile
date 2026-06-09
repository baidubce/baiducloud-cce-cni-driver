# FROM registry.baidubce.com/cce-plugin-dev/ubuntu-net-22-04:1.1
FROM registry.baidubce.com/cce-plugin-dev/ubuntu-net-22-04:1.2

LABEL maintainer="ZhongZhaowei<zhongzhaowei@baidu.com>"
WORKDIR /home/cce




# install cce node agent binary
COPY output/bin/cmd/agent /bin/agent

# install cni binaries
COPY tools/cni /cni
COPY output/bin/plugins /cni
COPY deploy/dockerfile/install-cni.sh install-cni.sh

CMD ["agent"]