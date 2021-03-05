#!/bin/bash

export PATH=/opt/compiler/gcc-8.2/bin:$PATH

make -f Makefile $1
