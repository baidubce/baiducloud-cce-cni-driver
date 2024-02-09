#!/bin/bash

export PATH=/opt/compiler/gcc-8.2/bin:$PATH
make -f Makefile prepare compile  GOARCH=amd64
mv output amd64
make -f Makefile prepare compile GOARCH=arm64
mv output arm64
mkdir -p output

mv arm64 output/arm64
mv amd64 output/amd64