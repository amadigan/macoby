FROM debian:bookworm-slim AS build-kernel
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    crossbuild-essential-amd64 \
    crossbuild-essential-arm64 \
    gcc \
    g++ \
    gcc-aarch64-linux-gnu \
    g++-aarch64-linux-gnu \
    binutils-aarch64-linux-gnu \
    binutils-x86-64-linux-gnu \
    make \
    ncurses-dev \
    libssl-dev \
    flex \
    bison \
    bc \
    perl \
    bash \
    libelf-dev \
    zstd
WORKDIR /src
ARG KERNEL_VERSION=6.10.14
ADD https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git#v${KERNEL_VERSION} .

FROM build-kernel AS build-kernel-amd64
ENV ARCH=x86_64
ENV CROSS_COMPILE=x86_64-linux-gnu-
COPY build/x86_64/linux-config .config
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc)
WORKDIR /target/${ARCH}/pkgroot/share/railyard/linux
RUN cp /src/.config /target/${ARCH}/linux-config
RUN cp "/src/$(make -s -C /src image_name)" kernel

FROM build-kernel AS build-kernel-arm64
ENV ARCH=arm64
ENV CROSS_COMPILE=aarch64-linux-gnu-
COPY build/arm64/linux-config .config
ADD https://developer.apple.com/download/files/RosettaPatch.zip .
RUN unzip -p RosettaPatch.zip RosettaPatch/RosettaPatch.diff | patch -p1
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc)
WORKDIR /target/${ARCH}/pkgroot/share/railyard/linux
RUN cp /src/.config /target/${ARCH}/linux-config
RUN cp /src/arch/arm64/boot/Image kernel
RUN zstd --ultra -22 kernel -o /target/${ARCH}/kernel.zst

FROM golang:alpine AS build-go
ENV CGO_ENABLED=0
WORKDIR /target/arm64/sbin
WORKDIR /target/x86_64/sbin
WORKDIR /src
COPY go.mod go.sum ./

FROM build-go AS build-init-arm64
ENV GOARCH=arm64
RUN go mod download
COPY ./internal internal
ARG LD_FLAGS=""
RUN go build -ldflags "${LD_FLAGS}" -o "/target/sbin/init" ./internal/guest/init

FROM build-go AS build-init-amd64
ENV GOARCH=amd64
RUN go mod download
COPY ./internal internal
ARG LD_FLAGS=""
RUN go build -ldflags "${LD_FLAGS}" -o "/target/sbin/init" ./internal/guest/init

FROM --platform=linux/arm64 alpine:edge AS rootfs-arm64
# git and openssh-client are used by dockerd
ARG DOCKER_VERSION=""
RUN apk add --no-cache e2fsprogs btrfs-progs git openssh-client docker-engine${DOCKER_VERSION}
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
RUN rm -rf /home /media /sbin/init
COPY --from=build-init-arm64 /target/ /

FROM --platform=linux/amd64 alpine:edge AS rootfs-amd64
# git and openssh-client are used by dockerd
ARG DOCKER_VERSION=""
RUN apk add --no-cache e2fsprogs btrfs-progs git openssh-client qemu-aarch64 docker-engine${DOCKER_VERSION}
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
RUN rm -rf /home /media /sbin/init
COPY --from=build-init-amd64 /target/ /

FROM alpine AS mksquashfs
RUN apk add --no-cache squashfs-tools
ARG ZSTD_LEVEL=3
WORKDIR /rootfs

FROM mksquashfs AS build-rootfs-arm64
COPY --from=rootfs-arm64 / .
WORKDIR /target/arm64/pkgroot/share/railyard/linux
RUN mksquashfs /rootfs rootfs.img -no-exports -noappend -comp zstd -Xcompression-level ${ZSTD_LEVEL}

FROM mksquashfs AS build-rootfs-amd64
COPY --from=rootfs-amd64 / .
WORKDIR /target/x86_64/pkgroot/share/railyard/linux
RUN mksquashfs /rootfs rootfs.img -no-exports -noappend -comp zstd -Xcompression-level ${ZSTD_LEVEL}

FROM scratch AS build-arm64
WORKDIR /arm64
COPY --from=build-rootfs-arm64 /target/arm64/ .
COPY --from=build-kernel-arm64 /target/arm64/ .

FROM scratch AS build-amd64
WORKDIR /x86_64
COPY --from=build-rootfs-amd64 /target/x86_64/ .
COPY --from=build-kernel-amd64 /target/x86_64/ .

FROM scratch AS build-all
COPY --from=build-arm64 / .
COPY --from=build-amd64 / .

FROM alpine AS erofs
RUN apk add --no-cache alpine-sdk build-base autoconf automake libtool zstd-dev lz4-dev util-linux-dev zlib-dev
WORKDIR /build
ADD https://github.com/erofs/erofs-utils.git#v1.8.4 .
RUN ./autogen.sh && ./configure --with-libzstd && make -j $(nproc) -l $(nproc) && make install
WORKDIR /sysroot
COPY --from=sysroot / .
RUN mkfs.erofs -zzstd,level=22 /rootfs.img /sysroot

