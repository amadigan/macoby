# localpkg script for railyard

lp_pkg[name]=railyard
lp_pkg[repo]=${GITHUB_REPOSITORY}
lp_pkg[package_pattern]=railyard-*.zip

if [[ -n "${RELEASE_TAG}" ]]; then
		lp_pkg[release]="${RELEASE_TAG}"

		# TODO dynamically determine the package name and hash
fi
