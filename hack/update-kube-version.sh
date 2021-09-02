#!/bin/sh
set -euo pipefail

# Usage
# In order to update k8s.io/kubernetes, we also have to update modules at
# k8s.io/XXX.
#
# To update the Kubernetes dependencies, first look for a tag here:
# https://github.com/kubernetes/kubernetes/releases
#
# Then, run this command:
# ./update-kube-version.sh 1.22.1
#
# This will update the go.mod file with the required sub-dependency versions
# and only then update the kubernetes version.

VERSION=${1#"v"}
if [ -z "$VERSION" ]; then
    echo "Must specify version!"
    exit 1
fi
echo "Specified version ${VERSION}"

echo "Checking which modules need updating..."
MODS=($(
    curl -sS https://raw.githubusercontent.com/kubernetes/kubernetes/v${VERSION}/go.mod |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))
echo "Found ${#MODS[@]} modules"

i=0
for MOD in "${MODS[@]}"; do
    V=$(
        go mod download -json "${MOD}@kubernetes-${VERSION}" |
        sed -n 's|.*"Version": "\(.*\)".*|\1|p'
    )
    go mod edit "-replace=${MOD}=${MOD}@${V}"

	i=$[i + 1]
	echo "Updated ${MOD} (${i}/${#MODS[@]})"
done

echo "Lastly, updating k8s.io/kubernetes..."
go get "k8s.io/kubernetes@v${VERSION}"
go mod tidy

echo "Done!"
