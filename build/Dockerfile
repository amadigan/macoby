# option to use ECR or other image registry
ARG IMAGE_PREFIX=""
# edge is used for the latest version of docker-engine
ARG ALPINE_VERSION=3.21.3
# passed in by the build
ARG GO_VERSION=1

# build-go* builds the go binaries for the guest
FROM ${IMAGE_PREFIX}golang:${GO_VERSION}-alpine AS build-go
ENV CGO_ENABLED=0
WORKDIR /target
WORKDIR /src

FROM build-go AS build-go-arm64
ENV GOARCH=arm64
ARG GOBUILD_FLAGS=""
ARG GOBUILD_LDFLAGS=""
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=bind,target=. go build -o /target/init ${GOBUILD_FLAGS} -ldflags "${GOBUILD_LDFLAGS}" ./internal/guest/init
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=bind,target=. go build -o /target/rootprep ./tools/rootprep

FROM build-go AS build-go-amd64
ENV GOARCH=amd64
ARG GOBUILD_FLAGS=""
ARG GOBUILD_LDFLAGS=""
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=bind,target=. go build -o /target/init ${GOBUILD_FLAGS} -ldflags "${GOBUILD_LDFLAGS}" ./internal/guest/init
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=bind,target=. go build -o /target/rootprep ./tools/rootprep

# kbuild-native is the image used to build the kernel when no cross-compilation is needed (alpine based)
FROM ${IMAGE_PREFIX}alpine:${ALPINE_VERSION} AS kbuild-native
RUN apk add --no-cache build-base bc flex bison perl ncurses-dev libelf openssl-dev zstd xz 
ARG KERNEL_VERSION=6.10.14
WORKDIR /src
# the output here is referenced in kbuild-cross-* images via COPY --link
RUN wget -O - https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${KERNEL_VERSION}.tar.xz | tar --strip-components 1 -xJ

# kbuild-cross-* are the images used to build the kernel when cross-compilation is needed, they are based on debian
FROM ${IMAGE_PREFIX}debian:bookworm-slim AS kbuild-cross-arm64
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked --mount=type=cache,target=/var/lib/apt,sharing=locked \
		apt-get update && apt-get install -y --no-install-recommends \
    build-essential make flex bison bc perl bash zstd ncurses-dev libssl-dev libelf-dev \
    crossbuild-essential-arm64 gcc-aarch64-linux-gnu binutils-aarch64-linux-gnu
ENV ARCH=arm64
ENV CROSS_COMPILE=aarch64-linux-gnu-
COPY --link --from=kbuild-native /src /src

FROM ${IMAGE_PREFIX}debian:bookworm-slim AS kbuild-cross-amd64
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    build-essential make flex bison bc perl bash zstd ncurses-dev libssl-dev libelf-dev \
    crossbuild-essential-amd64 gcc-x86-64-linux-gnu binutils-x86-64-linux-gnu
ENV ARCH=x86_64
ENV CROSS_COMPILE=x86_64-linux-gnu-
COPY --link --from=kbuild-native /src /src

# kbuild-HOSTARCH-TARGETARCH is the image used to build the kernel for a specific target architecture
# this mapping sets the image to use for each host-target pair
FROM kbuild-native AS kbuild-arm64-arm64
FROM kbuild-native AS kbuild-amd64-amd64
FROM kbuild-cross-arm64 AS kbuild-amd64-arm64
FROM kbuild-cross-amd64 AS kbuild-arm64-amd64

FROM kbuild-${BUILDARCH}-arm64 AS build-kernel-arm64
WORKDIR /patch
ADD https://developer.apple.com/download/files/RosettaPatch.zip .
RUN unzip RosettaPatch.zip
WORKDIR /src
RUN patch -p1 < /patch/RosettaPatch/RosettaPatch.diff
COPY ./build/arm64/linux-config .config
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc) Image

FROM build-kernel-arm64 AS build-kernel-arm64-modules
RUN make -j $(nproc) -l $(nproc) modules
RUN make modules_install INSTALL_MOD_PATH=/target

FROM kbuild-${BUILDARCH}-amd64 AS build-kernel-amd64
WORKDIR /src
COPY ./build/amd64/linux-config .config
RUN make olddefconfig
RUN make -j $(nproc) -l $(nproc) bzImage

FROM build-kernel-amd64 AS build-kernel-amd64-modules
RUN make -j $(nproc) -l $(nproc) modules
RUN make modules_install INSTALL_MOD_PATH=/target

FROM --platform=linux/arm64 ${IMAGE_PREFIX}alpine:${ALPINE_VERSION} AS root-arm64
# use edge as fallback for latest version of docker-engine
RUN echo "http://dl-cdn.alpinelinux.org/alpine/edge/community" >> /etc/apk/repositories
ARG DOCKER_VERSION=""
# git and openssh-client are used by dockerd
RUN apk add --no-cache e2fsprogs e2fsprogs-extra btrfs-progs git openssh-client cni-plugins docker-engine${DOCKER_VERSION} \
    qemu-arm qemu-ppc64le qemu-s390x qemu-mips64el qemu-riscv64 qemu-i386 qemu-x86_64
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
COPY --link --from=build-kernel-arm64-modules /target/lib /lib
RUN depmod $(ls /lib/modules)
RUN rm -rf /home /media /sbin/init
COPY --link --from=build-go-arm64 /target/init /sbin/init
COPY --link ./build/sysctl.conf /etc/sysctl.conf
COPY --link --from=build-go-arm64 /target/rootprep /sbin/rootprep
RUN /sbin/rootprep /
RUN rm -f /sbin/rootprep

FROM --platform=linux/amd64 ${IMAGE_PREFIX}alpine:${ALPINE_VERSION} AS root-amd64
RUN echo "http://dl-cdn.alpinelinux.org/alpine/edge/community" >> /etc/apk/repositories
ARG DOCKER_VERSION=""
RUN apk add --no-cache e2fsprogs e2fsprogs-extra btrfs-progs git openssh-client cni-plugins docker-engine${DOCKER_VERSION} \
  qemu-arm qemu-ppc64le qemu-s390x qemu-mips64el qemu-riscv64 qemu-aarch64
RUN apk del apk-tools alpine-keys musl-utils scanelf libc-utils
COPY --link --from=build-kernel-amd64-modules /target/lib /lib
RUN depmod $(ls /lib/modules)
RUN rm -rf /home /media /sbin/init
COPY --link --from=build-go-amd64 /target/init /sbin/init
COPY --link ./build/sysctl.conf /etc/sysctl.conf
COPY --link --from=build-go-amd64 /target/rootprep /sbin/rootprep
RUN /sbin/rootprep /
RUN rm -f /sbin/rootprep

FROM ${IMAGE_PREFIX}alpine:${ALPINE_VERSION} AS build-image
RUN apk add --no-cache squashfs-tools

FROM build-image AS build-image-arm64
ARG ZSTD_LEVEL=3
WORKDIR /target
RUN --mount=type=bind,dst=/src,from=root-arm64 mksquashfs /src rootfs.img -no-exports -noappend -comp zstd -Xcompression-level ${ZSTD_LEVEL}

FROM build-image AS build-image-amd64
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
