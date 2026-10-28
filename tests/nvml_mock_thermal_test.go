//go:build nvml_mock
// +build nvml_mock

// Copyright (c) 2026, NVIDIA CORPORATION.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"tests/helpers"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const (
	nvmlMockNamespace = "nvml-mock"
	nvmlMockPodLabel  = "app.kubernetes.io/name=nvml-mock"
	// The mock profile slows down below 90C, so this is clear of every profile.
	nvmlMockHotTempC = "142"
	nvmlMockGPU      = "0"
	// The agent runs nvml-mock-ctl. On Kubernetes 1.29+ the chart makes it a native sidecar (an init
	// container), so Containers[0] would be the NRI container, which does not share the override file.
	nvmlMockAgentContainer = "node-agent"
	// The agent bind-mounts the driver root read-only at /var/lib/nvml-mock/driver/config inside its
	// own container. The same host directory is writable through the volume mounted under /host.
	nvmlMockOverridesFile = "/host/var/lib/nvml-mock/driver/config/overrides.yaml"
	// How long a healthy node must stay schedulable before the fault is injected.
	nvmlMockHealthyWindow = 30 * time.Second
)

// TestNVMLMockThermalMargin runs against nvml-mock GPUs and the GPU Operator DCGM
// (see tilt/nvml-mock/). Heating a GPU must cordon its node, and cooling it must uncordon it.
func TestNVMLMockThermalMargin(t *testing.T) {
	feature := features.New("NVML mock thermal margin").
		WithLabel("suite", "nvml-mock").
		WithLabel("component", "thermal-margin")

	var (
		nodeName string
		mockPod  v1.Pod
	)

	feature.Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := c.NewClient()
		require.NoError(t, err, "failed to create kubernetes client")

		mockPod = findNVMLMockPod(ctx, t, client)
		nodeName = mockPod.Spec.NodeName
		t.Logf("Using nvml-mock pod %s on node %s", mockPod.Name, nodeName)

		return ctx
	})

	feature.Assess("healthy GPU does not cordon the node", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := c.NewClient()
		require.NoError(t, err, "failed to create kubernetes client")

		require.Never(t, func() bool {
			return isCordoned(ctx, t, client, nodeName)
		}, nvmlMockHealthyWindow, helpers.WaitInterval, "node %s must stay schedulable while the GPU is healthy", nodeName)

		return ctx
	})

	feature.Assess("hot GPU cordons the node", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := c.NewClient()
		require.NoError(t, err, "failed to create kubernetes client")

		runNVMLMockCtl(ctx, t, c, mockPod, "temp", "--gpu", nvmlMockGPU, nvmlMockHotTempC)
		helpers.WaitForNodesCordonState(ctx, t, client, []string{nodeName}, true)

		return ctx
	})

	feature.Assess("cooled GPU uncordons the node", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := c.NewClient()
		require.NoError(t, err, "failed to create kubernetes client")

		runNVMLMockCtl(ctx, t, c, mockPod, "reset", "--gpu", nvmlMockGPU)
		helpers.WaitForNodesCordonState(ctx, t, client, []string{nodeName}, false)

		return ctx
	})

	feature.Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		// Clear the override even when an assessment failed, so later runs start healthy.
		runNVMLMockCtl(ctx, t, c, mockPod, "reset", "--gpu", nvmlMockGPU)

		return ctx
	})

	testEnv.Test(t, feature.Feature())
}

func findNVMLMockPod(ctx context.Context, t *testing.T, client klient.Client) v1.Pod {
	t.Helper()

	var pods v1.PodList

	require.Eventually(t, func() bool {
		err := client.Resources(nvmlMockNamespace).List(ctx, &pods, func(opts *metav1.ListOptions) { opts.LabelSelector = nvmlMockPodLabel })
		if err != nil {
			t.Logf("failed to list nvml-mock pods: %v", err)
			return false
		}

		return len(pods.Items) > 0 && pods.Items[0].Status.Phase == v1.PodRunning
	}, helpers.EventuallyWaitTimeout, helpers.WaitInterval, "an nvml-mock pod should be running")

	return pods.Items[0]
}

func isCordoned(ctx context.Context, t *testing.T, client klient.Client, nodeName string) bool {
	t.Helper()

	node, err := helpers.GetNodeByName(ctx, client, nodeName)
	if err != nil {
		t.Logf("failed to get node %s: %v", nodeName, err)
		return false
	}

	return node.Spec.Unschedulable
}

func runNVMLMockCtl(ctx context.Context, t *testing.T, c *envconf.Config, pod v1.Pod, args ...string) {
	t.Helper()

	command := append([]string{"nvml-mock-ctl", "--file", nvmlMockOverridesFile}, args...)
	stdout, stderr, err := helpers.ExecInPod(ctx, c.Client().RESTConfig(), pod.Namespace, pod.Name,
		nvmlMockAgentContainer, command)
	require.NoError(t, err, fmt.Sprintf("%v failed: stdout=%q stderr=%q", command, stdout, stderr))
}
