# Makefile for building and signing vz with entitlements
OUTPUT_DIR := ./build
BIN_DIR := $(OUTPUT_DIR)/bin
LD_FLAGS := ""
ZSTD_LEVEL := 3
ARCH := $(shell uname -m)
APPNAME := railyard

ifeq ($(BUILD),prod)
	LD_FLAGS := "-s -w"
	ZSTD_LEVEL := 22
endif

all: $(OUTPUT_DIR)/bin/railyard $(OUTPUT_DIR)/linux/rootfs-$(ARCH).sqsh $(OUTPUT_DIR)/linux/kernel-$(ARCH)

$(OUTPUT_DIR)/bin/railyard: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f)
	mkdir -p $(BIN_DIR)
	go build -o $@ -ldflags $(LD_FLAGS) ./cmd/railyard
	codesign --entitlements vz.entitlements -s - $@

$(OUTPUT_DIR)/linux/rootfs-$(ARCH).sqsh: Dockerfile $(shell find internal -type f)
	mkdir -p $(OUTPUT_DIR)/linux
	docker build --target=sysroot --build-arg LD_FLAGS=$(LD_FLAGS) -t $(APPNAME)-sysroot .
	docker create --name $(APPNAME)-sysroot $(APPNAME)-sysroot
	docker export -o $(OUTPUT_DIR)/sysroot.tar $(APPNAME)-sysroot
	docker rm $(APPNAME)-sysroot
	mksquashfs - $@ -no-exports -noappend	-comp zstd -Xcompression-level $(ZSTD_LEVEL) -tar -e .dockerenv < $(OUTPUT_DIR)/sysroot.tar
	rm $(OUTPUT_DIR)/sysroot.tar

$(OUTPUT_DIR)/linux/kernel-$(ARCH): Dockerfile Kconfig
	mkdir -p $(OUTPUT_DIR)/linux
	docker build --target=kernel -t $(APPNAME)-kernel .
	docker create --name $(APPNAME)-kernel $(APPNAME)-kernel /bin/false
	docker cp $(APPNAME)-kernel:/linux $@
	docker cp $(APPNAME)-kernel:/Kconfig $(OUTPUT_DIR)/Kconfig
	docker rm $(APPNAME)-kernel

kconf-image: Dockerfile Kconfig
	docker build --target=kernel-config -t $(APPNAME)-kconf .

kconf: kconf-image
	docker run -it --rm --mount type=bind,src=$(PWD),dst=/project $(APPNAME)-kconf
