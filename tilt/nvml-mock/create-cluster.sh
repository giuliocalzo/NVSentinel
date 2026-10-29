#!/usr/bin/env bash
# Copyright (c) 2026, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Create the Kind cluster from default.kind.yaml with a local registry, without ctlptl.
# Follows https://kind.sigs.k8s.io/docs/user/local-registry/: Tilt and ko push images to
# localhost:5001, and the nodes pull them through the registry container.
#
# Run from the repository root.
set -euo pipefail

REG_NAME="kind-registry"
REG_PORT="5001"
KIND_CONFIG="tilt/nvml-mock/default.kind.yaml"
CLUSTER_NAME="$(yq eval '.name' "${KIND_CONFIG}")"

if [ "$(docker inspect -f '{{.State.Running}}' "${REG_NAME}" 2>/dev/null || true)" != "true" ]; then
  docker run -d --restart=always -p "127.0.0.1:${REG_PORT}:5000" --network bridge --name "${REG_NAME}" registry:2
fi

kind create cluster --config "${KIND_CONFIG}"

# Tell containerd on every node to resolve localhost:5001 to the registry container.
registry_dir="/etc/containerd/certs.d/localhost:${REG_PORT}"
for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
  docker exec "${node}" mkdir -p "${registry_dir}"
  printf '[host."http://%s:5000"]\n' "${REG_NAME}" | docker exec -i "${node}" cp /dev/stdin "${registry_dir}/hosts.toml"
done

if [ "$(docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${REG_NAME}")" = "null" ]; then
  docker network connect kind "${REG_NAME}"
fi

# Tilt reads this ConfigMap to find the registry it should push to.
kubectl apply -f - <<MANIFEST
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REG_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
MANIFEST
