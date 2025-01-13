# Makefile for building and signing vz with entitlements
OUTPUT_DIR := ./dist
BIN_DIR := $(OUTPUT_DIR)/bin
LD_FLAGS := ""
ZSTD_LEVEL := 3
ARCH := $(shell uname -m)
APPNAME := railyard
INSTALL_DIR := $(HOME)/Library/Application\ Support/$(APPNAME)

ifeq ($(BUILD),prod)
	LD_FLAGS := "-s -w"
	ZSTD_LEVEL := 22
endif

DOCKER_FLAGS := --build-arg ZSTD_LEVEL=$(ZSTD_LEVEL) --build-arg LD_FLAGS=$(LD_FLAGS)

ifneq ($(CI_CACHE),)
	DOCKER_FLAGS += --cache-to type=local,dest=$(CI_CACHE),mode=max,compression=zstd,compression-level=22,force-compression=true
endif

all: $(OUTPUT_DIR)/$(ARCH)/pkgroot/bin/railyard $(OUTPUT_DIR)/$(ARCH)/pkgroot/share/linux/rootfs.img

railyard: $(OUTPUT_DIR)/bin/railyard-$(ARCH)

$(OUTPUT_DIR)/arm64/pkgroot/bin/railyard: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f) build/vz.entitlements 
	mkdir -p $(OUTPUT_DIR)/arm64/pkgroot/bin
	GOARCH=arm64 GOOS=darwin go build -o $@ -ldflags $(LD_FLAGS) ./cmd/railyard
	codesign --entitlements ./build/vz.entitlements -s - $@ || true

$(OUTPUT_DIR)/x86_64/pkgroot/bin/railyard: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f) build/vz.entitlements 
	mkdir -p $(OUTPUT_DIR)/x86_64/pkgroot/bin
	GOARCH=amd64 GOOS=darwin go build -o $@ -ldflags $(LD_FLAGS) ./cmd/railyard
	codesign --entitlements ./build/vz.entitlements -s - $@ || true

$(OUTPUT_DIR)/arm64/pkgroot/share/linux/rootfs.img: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f) build/arm64/linux-config
	docker build $(DOCKER_FLAGS) -o type=local,dest=$(OUTPUT_DIR) --target build-arm64 .

$(OUTPUT_DIR)/x86_64/pkgroot/share/linux/rootfs.img: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f) build/x86_64/linux-config
	docker build $(DOCKER_FLAGS) -o type=local,dest=$(OUTPUT_DIR) --target build-amd64 .

$(OUTPUT_DIR)/$(APPNAME)-arm64.pkg: $(OUTPUT_DIR)/arm64/pkgroot/bin/railyard $(OUTPUT_DIR)/arm64/pkgroot/share/linux/rootfs.img
	mkdir -p $(OUTPUT_DIR)
	cp configs/railyard.jsonc dist/arm64/pkgroot/share/railyard/railyard.jsonc
	productbuild --identifier $(APPNAME) --version 1.0 --product build/arm64/requirements.plist --root dist/arm64/pkgroot /usr/local $@

$(OUTPUT_DIR)/$(APPNAME)-x86_64.pkg: $(OUTPUT_DIR)/x86_64/pkgroot/bin/railyard $(OUTPUT_DIR)/x86_64/pkgroot/share/linux/rootfs.img
	mkdir -p $(OUTPUT_DIR)
	cp configs/railyard.jsonc dist/x86_64/pkgroot/share/railyard/railyard.jsonc
	productbuild --identifier $(APPNAME) --version 1.0 --product build/x86_64/requirements.plist --root dist/x86_64/pkgroot /usr/local $@

packages: both $(OUTPUT_DIR)/$(APPNAME)-arm64.pkg $(OUTPUT_DIR)/$(APPNAME)-x86_64.pkg

both: $(shell find internal -type f) $(shell find cmd -type f) $(shell find cli -type f) build/x86_64/linux-config build/arm64/linux-config
	docker build --build-arg ZSTD_LEVEL=$(ZSTD_LEVEL) --build-arg LD_FLAGS=$(LD_FLAGS) -o type=local,dest=$(OUTPUT_DIR) --target build-all .

kconf-image: ./build/arm64/linux-config ./build/x86_64/linux-config Dockerfile
	docker build --target build-kernel -t $(APPNAME)-kernel .

kconf: kconf-image
	docker run --rm -it --mount type=bind,source=$(PWD)/build,target=/build $(APPNAME)-kernel

install:
	mkdir -p $(INSTALL_DIR)/linux
	cp -r $(OUTPUT_DIR)/$(ARCH)/pkgroot/share/railyard/linux $(INSTALL_DIR)
	cp -r $(OUTPUT_DIR)/$(ARCH)/pkgroot/bin $(INSTALL_DIR)
