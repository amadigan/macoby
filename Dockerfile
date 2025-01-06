FROM alpine AS kernel-config
WORKDIR /build
ADD https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git#v6.12.8 .
RUN apk update && apk add alpine-sdk sudo build-base autoconf automake libtool bison tar flex xz \
      elfutils-dev patch diffutils findutils lz4 ncurses-dev
# COPY Kconfig-base .config

FROM kernel-config AS kernel-build
# RUN make olddefconfig && make -j $(nproc) -l $(nproc)
COPY Kconfig .config
RUN make olddefconfig && make -j $(nproc) -l $(nproc)

FROM scratch AS kernel
COPY --from=kernel-build /build/arch/arm64/boot/Image /linux
COPY --from=kernel-build /build/.config /Kconfig

FROM golang AS build
ENV CGO_ENABLED=0
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY ./internal internal
ARG LD_FLAGS=""
RUN go build -ldflags "${LD_FLAGS}" -o ./init ./internal/guest/init

FROM alpine:edge AS sysroot
# git and openssh-client are used by dockerd
RUN apk add --no-cache docker-engine e2fsprogs btrfs-progs git openssh-client
RUN rm /sbin/init
COPY --from=build /build/init /sbin/init

