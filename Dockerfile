FROM alpine AS kernel-config
RUN apk update && apk add alpine-sdk abuild git sudo build-base autoconf automake libtool bison tar flex bison xz \
      elfutils-dev rsync openssl openssl-dev patch diffutils findutils lz4 python3 ncurses-dev
RUN git config --global advice.detachedHead false
WORKDIR /build
COPY local/cache/git/linux.git .git/
RUN git config --unset core.bare && git checkout v6.12
COPY Kconfig-base .config

FROM kernel-config AS kernel-build
RUN make olddefconfig && make -j $(nproc) -l $(nproc)
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
COPY ./cmd cmd
ARG init
RUN go build -o ./init ./cmd/${init}

FROM alpine AS sysroot
WORKDIR /overlay
RUN apk add --no-cache docker-engine e2fsprogs
COPY conf/dockerd.json /etc/docker/daemon.json
COPY conf/macoby.json /etc/macoby.json
COPY --from=build /build/init /bin/init

