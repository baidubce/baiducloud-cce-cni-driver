FROM registry.baidubce.com/cce-plugin-dev/ubuntu-net-22-04:1.1

LABEL maintainer="WeiweiWang<wangweiwei22@baidu.com>"
WORKDIR /home/cce


COPY output/bin/operator/cce-operator-rdma /bin/cce-network-operator
COPY output/bin/cmd/webhook /bin/webhook


CMD ["/bin/cce-network-operator"]