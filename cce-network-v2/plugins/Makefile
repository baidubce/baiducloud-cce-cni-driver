# Copyright Authors of CCE
# SPDX-License-Identifier: Apache-2.0

include ../Makefile.defs

TARGETS := enim exclusive-device sbr-eip cipam cptp endpoint-probe roce exclusive-rdma

.PHONY: all $(TARGETS) clean install

all: $(TARGETS)

$(TARGETS):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $(PWD)/output/bin/plugins/$(@) ./$(@)

$(TARGET):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $@

clean:
	@$(ECHO_CLEAN)
	$(foreach target,$(TARGETS), $(QUIET)rm -f $(PWD)/output/bin/plugins/$(target))
	$(GO) clean $(GOCLEAN)

install:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(foreach target,$(TARGETS), $(QUIET)$(INSTALL) -m 0755 $(target) $(DESTDIR)$(BINDIR);)
