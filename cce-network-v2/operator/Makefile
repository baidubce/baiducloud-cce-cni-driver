# Copyright Authors of CCE
# SPDX-License-Identifier: Apache-2.0

include ../Makefile.defs

#TARGETS := cce-operator cce-operator-generic cce-operator-aws cce-operator-azure cce-operator-alibabacloud
TARGETS := cce-network-operator cce-network-operator-generic cce-network-operator-pcb cce-operator-vpc-eni cce-operator-rdma

.PHONY: all $(TARGETS) clean install

all: $(TARGETS)

cce-network-operator: GO_TAGS_FLAGS+=ipam_provider_operator,ipam_provider_vpc_eni,ipam_provider_vpc_route,ipam_provider_rdma
cce-network-operator-generic: GO_TAGS_FLAGS+=ipam_provider_operator
cce-operator-vpc-eni: GO_TAGS_FLAGS+=ipam_provider_vpc_eni,ipam_provider_vpc_route,ipam_provider_rdma
cce-operator-rdma: GO_TAGS_FLAGS+=ipam_provider_rdma
cce-network-operator-pcb: GO_TAGS_FLAGS+=ipam_provider_private_cloud_base

$(TARGETS):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $(PWD)/output/bin/operator/$(@)

$(TARGET):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $@

clean:
	@$(ECHO_CLEAN)
	$(foreach target,$(TARGETS), $(QUIET)rm -f $(PWD)/output/bin/operator/$(target))
	$(GO) clean $(GOCLEAN)

install:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(foreach target,$(TARGETS), $(QUIET)$(INSTALL) -m 0755 $(target) $(DESTDIR)$(BINDIR);)
