# Makefile for building and signing vz with entitlements

BIN_DIR := ./local/bin
BINARY := $(BIN_DIR)/vz

all: build sign

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BINARY) ./tools/vz

sign:
	codesign --entitlements vz.entitlements -s - $(BINARY)

local/sysroot.squashfs: Dockerfile cmd/**/* internal/**/*
	docker build --build-arg init=vzguest --target=sysroot -t macoby-sysroot .
	docker create --name macoby-sysroot macoby-sysroot
	docker export -o local/sysroot.tar macoby-sysroot
	docker rm macoby-sysroot
	mksquashfs - local/sysroot.squashfs -noappend	-comp lz4 -Xhc -tar -e .dockerenv < local/sysroot.tar
	rm local/sysroot.tar

local/linux: Dockerfile Kconfig
	docker build --target=kernel -t macoby-kernel .
	docker create --name macoby-kernel macoby-kernel /bin/false
	docker cp macoby-kernel:/linux local/linux
	docker cp macoby-kernel:/Kconfig local/Kconfig
	docker rm macoby-kernel
