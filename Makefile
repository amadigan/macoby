# Makefile for building and signing vz with entitlements

BIN_DIR := ./local/bin
BINARY := $(BIN_DIR)/vz
LD_FLAGS := ""
ZSTD_LEVEL := 3

ifeq ($(BUILD),prod)
	LD_FLAGS := "-s -w"
	ZSTD_LEVEL := 22
endif

all: build sign

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BINARY) -ldflags "-s -w" ./tools/vz

sign:
	codesign --entitlements vz.entitlements -s - $(BINARY)

local/sysroot.sqsh: Dockerfile $(shell find internal -type f)
	docker build --target=sysroot --build-arg LD_FLAGS=$(LD_FLAGS) -t macoby-sysroot .
	docker create --name macoby-sysroot macoby-sysroot
	docker export -o local/sysroot.tar macoby-sysroot
	docker rm macoby-sysroot
	mksquashfs - local/sysroot.sqsh -no-exports -noappend	-comp zstd -Xcompression-level $(ZSTD_LEVEL) -tar -e .dockerenv < local/sysroot.tar
	rm local/sysroot.tar

local/linux: Dockerfile Kconfig
	docker build --target=kernel -t macoby-kernel .
	docker create --name macoby-kernel macoby-kernel /bin/false
	docker cp macoby-kernel:/linux local/linux
	docker cp macoby-kernel:/Kconfig local/Kconfig
	docker rm macoby-kernel

kconf-image: Dockerfile Kconfig
	docker build --target=kernel-config -t macoby-kconf .

kconf: kconf-image
	docker run -it --rm --mount type=bind,src=$(PWD),dst=/project macoby-kconf
