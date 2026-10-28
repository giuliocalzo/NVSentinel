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

# Deploy NVSentinel on the nvml-mock cluster with `tilt ci`, with retries.
# The nvml-mock agent restarts containerd on each worker, so an image pull at that moment fails. Before a
# retry, delete the pods stuck on an image pull and remove half-installed Helm releases.
#
# Run from the repository root.
set -euo pipefail

export USE_NVML_MOCK=1 USE_KWOK=0
ATTEMPTS="${ATTEMPTS:-3}"
TIMEOUT="${TIMEOUT:-20m}"

for i in $(seq 1 "${ATTEMPTS}"); do
  echo "Attempt ${i} of ${ATTEMPTS}..."
  if tilt ci -f tilt/Tiltfile --timeout="${TIMEOUT}"; then
    exit 0
  fi
  if [ "${i}" -lt "${ATTEMPTS}" ]; then
    echo "Tilt CI failed on attempt ${i}, cleaning up and retrying in 30 seconds..."
    kubectl get pods --all-namespaces --output json \
      | jq -r '.items[] | select([.status.containerStatuses[]?, .status.initContainerStatuses[]?
          | .state.waiting.reason] | any(. == "ErrImagePull" or . == "ImagePullBackOff"))
          | "\(.metadata.namespace) \(.metadata.name)"' \
      | while read -r namespace name; do kubectl delete pod "${name}" --namespace "${namespace}" --wait=false; done
    helm list --all-namespaces --pending --output json \
      | jq -r '.[] | "\(.name) \(.namespace)"' \
      | while read -r name namespace; do helm uninstall "${name}" --namespace "${namespace}" || true; done
    sleep 30
  fi
done
echo "Tilt CI failed after ${ATTEMPTS} attempts"
exit 1
