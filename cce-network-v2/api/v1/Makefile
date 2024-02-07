# Copyright Authors of CCE
# SPDX-License-Identifier: Apache-2.0
include ../../Makefile.defs

# Update this via images/scripts/update-cce-builder-image.sh
CILIUM_BUILDER_IMAGE=registry.baidubce.com/cce-plugin-dev/cilium/cilium-builder:12.0

.PHONY: proto
proto:
	$(QUIET)$(CONTAINER_ENGINE) container run --rm \
		--volume $(CURDIR):/src \
		--user "$(shell id -u):$(shell id -g)" \
		$(CILIUM_BUILDER_IMAGE) \
		make -C /src -f Makefile.protoc
