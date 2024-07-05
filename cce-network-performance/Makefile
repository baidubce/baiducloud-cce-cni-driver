PWD=$(shell pwd)
SWAGGER=docker run --rm -v $(PWD):$(PWD) registry.baidubce.com/cce-plugin-dev/swaggerapi/swagger-codegen-cli:2.4.41

fmt:
	rustfmt cce-performance-network/...
generate:
	$(SWAGGER) generate -i $(PWD)/cce-network-v2/api/v1/openapi.yaml -l rust -o $(PWD)/cce-performance-network/cni-common -DpackageName=cni-common