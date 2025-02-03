# option to use ECR or other image registry
ARG IMAGE_PREFIX=""
# edge is used for the latest version of docker-engine
ARG ALPINE_SYSTEM_VERSION=edge
# passed in by the build
ARG GO_VERSION=1

FROM ${IMAGE_PREFIX}alpine AS build-tools
RUN apk add --no-cache squashfs-tools

FROM ${IMAGE_PREFIX}golang:${GO_VERSION}-alpine AS build-go
ENV CGO_ENABLED=0
WORKDIR /target
WORKDIR /src
COPY go.mod go.sum ./

FROM build-go AS build-init-arm64
ENV GOARCH=arm64
ENV GOBUILD_FLAGS=""
ENV GOBUILD_LDFLAGS=""
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=bind,target=. go build -o /target/init ${GOBUILD_FLAGS} -ldflags "${GOBUILD_LDFLAGS}" ./internal/guest/init

FROM build-go AS build-init-amd64
ENV GOARCH=amd64
ENV GOBUILD_FLAGS=""
ENV GOBUILD_LDFLAGS=""
RUN --mount=type=cache,target=/go/pkg/mod  --mount=type=bind,target=. go build -o /target/init ${GOBUILD_FLAGS} -ldflags "${GOBUILD_LDFLAGS}" ./internal/guest/init

FROM ${IMAGE_PREFIX}debian:bookworm-slim AS build-kernel
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked --mount=type=cache,target=/var/lib/apt,sharing=locked \
		apt-get update && apt-get install -y --no-install-recommends \
    build-essential make flex bison bc perl bash zstd \
    crossbuild-essential-amd64 gcc-x86-64-linux-gnu binutils-x86-64-linux-gnu \
    crossbuild-essential-arm64 gcc-aarch64-linux-gnu binutils-aarch64-linux-gnu \
    ncurses-dev libssl-dev libelf-dev xz-utils
WORKDIR /src
ARG KERNEL_VERSION=6.12.11
ADD --link https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${KERNEL_VERSION}.tar.xz /linux.tar.xz
RUN tar --strip-components=1 -xJf /linux.tar.xz

FROM build-kernel AS build-kernel-arm64
WORKDIR /build
COPY ./build/arm64/rosetta rosetta
WORKDIR /src
RUN patch -p1 < /build/rosetta/rosetta-patch.diff
ENV ARCH=arm64
ENV CROSS_COMPILE=aarch64-linux-gnu-
COPY ./build/arm64/linux-config .config
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc) Image

FROM build-kernel-arm64 AS build-kernel-arm64-modules
RUN make -j $(nproc) -l $(nproc) modules
RUN make modules_install INSTALL_MOD_PATH=/target

FROM build-kernel AS build-kernel-amd64
COPY --link ./build/amd64 /build
WORKDIR /src
RUN cp /build/linux-config .config
ENV ARCH=x86_64
ENV CROSS_COMPILE=x86_64-linux-gnu-
COPY ./build/amd64/linux-config .config
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc) bzImage

FROM build-kernel-amd64 AS build-kernel-amd64-modules
RUN make -j $(nproc) -l $(nproc) modules
RUN make modules_install INSTALL_MOD_PATH=/target

FROM --platform=linux/arm64 ${IMAGE_PREFIX}alpine:${ALPINE_SYSTEM_VERSION} AS root-arm64
# git and openssh-client are used by dockerd
ARG DOCKER_VERSION=""
RUN apk add --no-cache e2fsprogs btrfs-progs git openssh-client docker-engine${DOCKER_VERSION}
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
COPY --link --from=build-kernel-arm64-modules /target/lib /lib
RUN depmod $(ls /lib/modules)
RUN rm -rf /home /media /sbin/init
COPY --link --from=build-init-arm64 /target/init /sbin/init

FROM --platform=linux/amd64 ${IMAGE_PREFIX}alpine:${ALPINE_SYSTEM_VERSION} AS root-amd64
# git and openssh-client are used by dockerd
ARG DOCKER_VERSION=""
# note qemu-aarch64 for arm64 binary translation
RUN apk add --no-cache e2fsprogs btrfs-progs git openssh-client qemu-aarch64 docker-engine${DOCKER_VERSION}
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
COPY --link --from=build-kernel-amd64-modules /target/lib /lib
RUN depmod $(ls /lib/modules)
RUN rm -rf /home /media /sbin/init
COPY --link --from=build-init-amd64 /target/init /sbin/init

FROM build-tools AS build-image-arm64
ARG ZSTD_LEVEL=3
WORKDIR /target
RUN --mount=type=bind,dst=/src,from=root-arm64 mksquashfs /src rootfs.img -no-exports -noappend -comp zstd -Xcompression-level ${ZSTD_LEVEL}

FROM build-tools AS build-image-amd64
ARG ZSTD_LEVEL=3
WORKDIR /target
RUN --mount=type=bind,dst=/src,from=root-amd64 mksquashfs /src rootfs.img -no-exports -noappend -comp zstd -Xcompression-level ${ZSTD_LEVEL}

FROM scratch AS rootfs-arm64
WORKDIR /share/railyard/linux
COPY --from=build-kernel-arm64 /src/.config /linux-config
COPY --from=build-kernel-arm64 /src/arch/arm64/boot/Image ./kernel
COPY --from=build-image-arm64 /target/rootfs.img ./rootfs.img

FROM scratch AS rootfs-amd64
WORKDIR /share/railyard/linux
COPY --from=build-kernel-arm64 /src/.config /linux-config
COPY --from=build-kernel-amd64 /src/arch/x86_64/boot/bzImage ./kernel
COPY --from=build-image-amd64 /target/rootfs.img ./rootfs.img

FROM scratch AS dist
WORKDIR /arm64
COPY --link --from=rootfs-arm64 /* ./
WORKDIR /amd64
COPY --link --from=rootfs-amd64 /* ./
