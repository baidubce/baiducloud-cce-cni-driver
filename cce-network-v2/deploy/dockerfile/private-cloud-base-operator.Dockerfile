FROM registry.baidubce.com/cce-plugin-pro/cce-cni-base:v1.0.0

LABEL maintainer="WeiweiWang<wangweiwei22@baidu.com>"
WORKDIR /home/cce


COPY output/bin/operator/cce-ipam-operator-pcb /bin/cce-ipam-operator-pcb
COPY output/bin/cmd/webhook /bin/webhook


CMD ["/bin/cce-ipam-operator-pcb"]