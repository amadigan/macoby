#!/bin/zsh
typeset -a docker_args=(
	--rm
	--interactive
	--pid=host
	--network=host
	--mount type=bind,source=/,target=/sysroot
	--privileged
)

[[ -t 0 ]] && docker_args+=(--tty)

docker run "${(@)docker_args}" alpine chroot /sysroot
