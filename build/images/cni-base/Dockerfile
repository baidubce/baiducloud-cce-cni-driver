FROM alpine:3.18

LABEL maintainer="Chen Yaqi<chenyaqi01@baidu.com>"

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories 
RUN apk --no-cache add curl

# ensure cni bin from https://github.com/containernetworking/plugins/releases/download/v1.0.1/cni-plugins-linux-${arch}-v1.0.1.tgz
RUN arch=$(arch | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) && \
  curl -L -k --retry 5 https://github.com/containernetworking/plugins/releases/download/v1.0.1/cni-plugins-linux-${arch}-v1.0.1.tgz | tar -xz -C / ./loopback ./host-local ./portmap ./bridge

# install entrypoint.sh and uninstall-cni.sh
COPY entrypoint.sh /entrypoint.sh
COPY uninstall-cni.sh /uninstall-cni.sh

CMD ["/bin/sh", "/entrypoint.sh"]
