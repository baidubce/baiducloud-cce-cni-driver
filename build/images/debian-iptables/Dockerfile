FROM debian:buster

# Install latest iptables package from buster-backports
RUN echo deb http://deb.debian.org/debian buster-backports main >> /etc/apt/sources.list; \
    apt-get update; \
    apt-get -t buster-backports -y --no-install-recommends install iptables

# Install other dependencies and then clean up apt caches
RUN apt-get install -y --no-install-recommends \
    conntrack \
    ipset \
    ipvsadm \
    kmod \
    netbase \
    tcpdump \
    iproute2 \
    net-tools \
    dnsutils \
    curl \
    traceroute \
    inetutils-ping \
    telnet \
    netcat \
    ethtool \
    procps \
    silversearcher-ag \
    less

# Cleanup
RUN apt-get clean -y
RUN rm -rf \
   /var/cache/debconf/* \
   /var/lib/apt/lists/* \
   /var/log/* \
   /tmp/* \
   /var/tmp/*

# Install iptables wrapper scripts to detect the correct iptables mode
# the first time any of them is run
COPY iptables-wrapper-installer.sh /
RUN /iptables-wrapper-installer.sh
