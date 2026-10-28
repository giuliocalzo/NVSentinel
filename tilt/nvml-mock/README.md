# Tilt with nvml-mock

This folder runs the Tilt e2e stack on mock GPUs from [nvml-mock](https://github.com/NVIDIA/k8s-test-infra/tree/main/docs/guides/nv-sentinel) and the official NVIDIA GPU Operator, instead of the fake DCGM DaemonSet. Tilt builds the NVSentinel images from this repository, so the test covers the code under review.

```bash
tilt/nvml-mock/create-cluster.sh
tilt/nvml-mock/deploy.sh
cd tests && go test -tags nvml_mock -run TestNVMLMock ./...
```

- `Tiltfile` is included by `tilt/Tiltfile` when `USE_NVML_MOCK=1`. It replaces the fake driver and DCGM DaemonSets with the published nvml-mock chart (0.5.1, prebuilt image, nothing is built) and the official GPU Operator with a standalone DCGM.
- `NVML_MOCK_VERSION` (default `0.5.1`), `GPU_PROFILE` (default `gb300`) and `GPU_OPERATOR_VERSION` (default `v26.3.3`) override the chart versions and the mock profile.
- `gpu-operator-values.yaml` is an unmodified copy of the k8s-test-infra NVSentinel guide values. It enables the standalone DCGM.
- `values-nvsentinel.yaml` is the NVSentinel values overlay that `USE_NVML_MOCK=1` adds. It disables the CSP, syslog and NIC monitors and lowers the largest CPU requests, so NVSentinel fits one control plane on a 4-CPU runner. `USE_NVML_MOCK=1` also skips `simple-health-client` and `csp-api-mock` in Tilt.
- `create-cluster.sh` creates the Kind cluster from `default.kind.yaml` with a local registry on `localhost:5001`, without ctlptl.
- `deploy.sh` runs `tilt ci` with `USE_NVML_MOCK=1 USE_KWOK=0` and retries. The nvml-mock agent restarts containerd on each worker, so a pull at that moment fails. Before a retry, the script deletes the pods stuck on an image pull and removes half-installed Helm releases.
- `default.kind.yaml` is a cut-down copy of k8s-test-infra `local/kind/default.kind.yaml`: one control plane and two workers. The `nvml-mock.nvidia.com/type=sgpu` worker label is what the nvml-mock node selector uses.
- The `nvml_mock_thermal_test.go` test in `tests/` heats a GPU, expects a cordon, then cools it and expects an uncordon.
- The CI job is `.github/workflows/nvml-mock-e2e.yml`.
