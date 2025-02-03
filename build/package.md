# Railyard Packaging Notes

## Build Timestamp

Railyard does not use a consistent build timestamp, it isn't practical to do so because the railyard executable is signed at build time. However, the virtual
machine image does have a concept of a build timestamp, and this timestamp is used for the kernel and the root filesystem. The build timestamp is stored in the
railyard executable, along with the git version, commit ID, and dirty flag.

## Packaging

Railyard is packaged in two forms: a .pkg Installer Package and a ZIP archive (with extensions). Separate packages are generated for each target architecture.
The formal name of the package is railyard-version-arch.ext where version is the version number, arch is the target architecture, and ext is the file extension.

Note that while internally, railyard uses the go name of the target architecture (e.g. arm64, amd64), the package name for amd64 is x86_64. This is to match the
convention used by Apple for the architecture name.

The packages contain the following files:

/bin/railyard - executable (permissions 555)
/share/railyard/linux/kernel - kernel image (permissions 444)
/share/railyard/linux/rootfs.img - root filesystem image (permissions 444)

Uncompressed, this totals nearly 200MB installed. For the signed and notarized .pkg files, compression is left up to
productbuild(1). However, for the ZIP archive, special processing is used:

- The executable is compressed with xz
- On arm64, the kernel is compressed with xz (the amd64 kernel is already compressed with Zstd)
- The root filesystem is not compressed (it is already compressed with Zstd)
- The LICENSE file is added at the root of the archive (permissions 644, xz compressed)

The files always appear in the order listed above (LICENSE file last), with no directory entries.

Note that bsdtar on macOS supports XZ compression (mode 95) in zip files, it does not support Zstd compression. 

## localpkg Script

The localpkg script is used to build a curlable installer for the ZIP package on macOS. The script extracts some
information from the build environment at runtime:

- The release is determined with `git describe --tags --always`
- The repo is GITHUB_REPOSITORY
- The name is the last component of the repo
