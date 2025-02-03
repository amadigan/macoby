#!/bin/zsh
set -e

cd "${ZSH_SCRIPT:h}/.."
docker build --target build-kernel -t railyard-kconf -f build/Dockerfile .
echo exit $?
docker run --rm -it --mount type=bind,source="$(pwd)",target=/project railyard-kconf
