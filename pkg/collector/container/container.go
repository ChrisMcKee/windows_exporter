//go:build windows

package container

import (
	"context"
	"fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/docker/docker/client"
	"os"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus-community/windows_exporter/pkg/perflib"
	"github.com/prometheus-community/windows_exporter/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
)

const Name = "container"

type Config struct{}

var ConfigDefaults = Config{}

// A collector is a Prometheus collector for containers metrics
type collector struct {
	logger log.Logger

	// Presence
	ContainerAvailable *prometheus.Desc

	// Number of containers
	ContainersCount *prometheus.Desc
	// memory
	UsageCommitBytes            *prometheus.Desc
	UsageCommitPeakBytes        *prometheus.Desc
	UsagePrivateWorkingSetBytes *prometheus.Desc

	// CPU
	RuntimeTotal  *prometheus.Desc
	RuntimeUser   *prometheus.Desc
	RuntimeKernel *prometheus.Desc

	// Network
	BytesReceived          *prometheus.Desc
	BytesSent              *prometheus.Desc
	PacketsReceived        *prometheus.Desc
	PacketsSent            *prometheus.Desc
	DroppedPacketsIncoming *prometheus.Desc
	DroppedPacketsOutgoing *prometheus.Desc

	// Storage
	ReadCountNormalized  *prometheus.Desc
	ReadSizeBytes        *prometheus.Desc
	WriteCountNormalized *prometheus.Desc
	WriteSizeBytes       *prometheus.Desc
}

type ContainerDetails struct {
	ContainerId   string
	ContainerName string
	Namespace     string
	PodName       string
	PodID         string
}

func isKubernetes() bool {
	// Injected by Kubernetes itself
	if os.Getenv("KUBERNETES_SERVICE_PORT") != "" && os.Getenv("OS") == "Windows_NT" {
		return true
	}
	return false
}

// New constructs a new collector
func New(logger log.Logger, _ *Config) types.Collector {
	c := &collector{}
	c.SetLogger(logger)
	return c
}

func NewWithFlags(_ *kingpin.Application) types.Collector {
	return &collector{}
}

func (c *collector) GetName() string {
	return Name
}

func (c *collector) SetLogger(logger log.Logger) {
	c.logger = log.With(logger, "collector", Name)
}

func (c *collector) GetPerfCounter() ([]string, error) {
	return []string{}, nil
}

func (c *collector) Build() error {
	var tags []string
	var networkingTags []string
	if isKubernetes() {
		tags = []string{"container_id", "container_name", "namespace", "pod_name", "pod_id"}
		networkingTags = []string{"container_id", "container_name", "namespace", "pod_name", "pod_id", "interface"}
	} else {
		tags = []string{"container_id", "container_name"}
		networkingTags = []string{"container_id", "container_name", "interface"}
	}

	c.ContainerAvailable = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "available"),
		"Available",
		tags,
		nil,
	)
	c.ContainersCount = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "count"),
		"Number of containers",
		nil,
		nil,
	)
	c.UsageCommitBytes = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "memory_usage_commit_bytes"),
		"Memory Usage Commit Bytes",
		tags,
		nil,
	)
	c.UsageCommitPeakBytes = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "memory_usage_commit_peak_bytes"),
		"Memory Usage Commit Peak Bytes",
		tags,
		nil,
	)
	c.UsagePrivateWorkingSetBytes = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "memory_usage_private_working_set_bytes"),
		"Memory Usage Private Working Set Bytes",
		tags,
		nil,
	)
	c.RuntimeTotal = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "cpu_usage_seconds_total"),
		"Total Run time in Seconds",
		tags,
		nil,
	)
	c.RuntimeUser = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "cpu_usage_seconds_usermode"),
		"Run Time in User mode in Seconds",
		tags,
		nil,
	)
	c.RuntimeKernel = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "cpu_usage_seconds_kernelmode"),
		"Run time in Kernel mode in Seconds",
		tags,
		nil,
	)
	c.BytesReceived = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_receive_bytes_total"),
		"Bytes Received on Interface",
		networkingTags,
		nil,
	)
	c.BytesSent = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_transmit_bytes_total"),
		"Bytes Sent on Interface",
		networkingTags,
		nil,
	)
	c.PacketsReceived = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_receive_packets_total"),
		"Packets Received on Interface",
		networkingTags,
		nil,
	)
	c.PacketsSent = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_transmit_packets_total"),
		"Packets Sent on Interface",
		networkingTags,
		nil,
	)
	c.DroppedPacketsIncoming = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_receive_packets_dropped_total"),
		"Dropped Incoming Packets on Interface",
		networkingTags,
		nil,
	)
	c.DroppedPacketsOutgoing = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "network_transmit_packets_dropped_total"),
		"Dropped Outgoing Packets on Interface",
		networkingTags,
		nil,
	)
	c.ReadCountNormalized = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "storage_read_count_normalized_total"),
		"Read Count Normalized",
		tags,
		nil,
	)
	c.ReadSizeBytes = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "storage_read_size_bytes_total"),
		"Read Size Bytes",
		tags,
		nil,
	)
	c.WriteCountNormalized = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "storage_write_count_normalized_total"),
		"Write Count Normalized",
		tags,
		nil,
	)
	c.WriteSizeBytes = prometheus.NewDesc(
		prometheus.BuildFQName(types.Namespace, Name, "storage_write_size_bytes_total"),
		"Write Size Bytes",
		tags,
		nil,
	)
	return nil
}

// Collect sends the metric values for each metric
// to the provided prometheus Metric channel.
func (c *collector) Collect(_ *types.ScrapeContext, ch chan<- prometheus.Metric) error {
	if desc, err := c.collect(ch); err != nil {
		_ = level.Error(c.logger).Log("msg", "failed collecting collector metrics", "desc", desc, "err", err)
		return err
	}
	return nil
}

// containerClose closes the container resource
func (c *collector) containerClose(container hcsshim.Container) {
	err := container.Close()
	if err != nil {
		_ = level.Error(c.logger).Log("err", err)
	}
}

func (c *collector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	// Types Container is passed to get the containers compute systems only
	containers, err := hcsshim.GetContainers(hcsshim.ComputeSystemQuery{Types: []string{"Container"}})
	if err != nil {
		_ = level.Error(c.logger).Log("msg", "Err in Getting containers", "err", err)
		return nil, err
	}

	count := len(containers)

	ch <- prometheus.MustNewConstMetric(
		c.ContainersCount,
		prometheus.GaugeValue,
		float64(count),
	)
	if count == 0 {
		return nil, nil
	}

	containerLabelLookup := make(map[string][]string)

	for _, containerDetails := range containers {
		// https://stackoverflow.com/questions/45617758/proper-way-to-release-resources-with-defer-in-a-loop
		func() {
			container, err := hcsshim.OpenContainer(containerDetails.ID)
			if container != nil {
				defer c.containerClose(container)
			}
			if err != nil {
				_ = level.Error(c.logger).Log("msg", "err in opening container", "containerId", containerDetails.ID, "err", err)
				return
			}

			cstats, err := container.Statistics()
			if err != nil {
				_ = level.Error(c.logger).Log("msg", "err in fetching container Statistics", "containerId", containerDetails.ID, "err", err)
				return
			}

			containerLabels := getContainerLabels(containerDetails.ID, containerDetails.Owner)
			containerLabelLookup[containerDetails.ID] = containerLabels

			ch <- prometheus.MustNewConstMetric(
				c.ContainerAvailable,
				prometheus.CounterValue,
				1,
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.UsageCommitBytes,
				prometheus.GaugeValue,
				float64(cstats.Memory.UsageCommitBytes),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.UsageCommitPeakBytes,
				prometheus.GaugeValue,
				float64(cstats.Memory.UsageCommitPeakBytes),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.UsagePrivateWorkingSetBytes,
				prometheus.GaugeValue,
				float64(cstats.Memory.UsagePrivateWorkingSetBytes),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.RuntimeTotal,
				prometheus.CounterValue,
				float64(cstats.Processor.TotalRuntime100ns)*perflib.TicksToSecondScaleFactor,
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.RuntimeUser,
				prometheus.CounterValue,
				float64(cstats.Processor.RuntimeUser100ns)*perflib.TicksToSecondScaleFactor,
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.RuntimeKernel,
				prometheus.CounterValue,
				float64(cstats.Processor.RuntimeKernel100ns)*perflib.TicksToSecondScaleFactor,
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.ReadCountNormalized,
				prometheus.CounterValue,
				float64(cstats.Storage.ReadCountNormalized),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.ReadSizeBytes,
				prometheus.CounterValue,
				float64(cstats.Storage.ReadSizeBytes),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.WriteCountNormalized,
				prometheus.CounterValue,
				float64(cstats.Storage.WriteCountNormalized),
				containerLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.WriteSizeBytes,
				prometheus.CounterValue,
				float64(cstats.Storage.WriteSizeBytes),
				containerLabels...,
			)
		}()
	}

	hnsEndpoints, err := hcsshim.HNSListEndpointRequest()
	if err != nil {
		_ = level.Warn(c.logger).Log("msg", "Failed to collect network stats for containers")
		return nil, nil
	}

	if len(hnsEndpoints) == 0 {
		_ = level.Info(c.logger).Log("msg", fmt.Sprintf("No network stats for containers to collect"))
		return nil, nil
	}

	for _, endpoint := range hnsEndpoints {
		endpointStats, err := hcsshim.GetHNSEndpointStats(endpoint.Id)
		if err != nil {
			_ = level.Warn(c.logger).Log("msg", fmt.Sprintf("Failed to collect network stats for interface %s", endpoint.Id), "err", err)
			continue
		}

		for _, containerId := range endpoint.SharedContainers {

			containerLabels, ok := containerLabelLookup[containerId]
			endpointLabels := append(containerLabels, strings.ToUpper(endpoint.Id))

			if !ok {
				_ = level.Warn(c.logger).Log("msg", fmt.Sprintf("Failed to collect network stats for container %s", containerId))
				continue
			}

			ch <- prometheus.MustNewConstMetric(
				c.BytesReceived,
				prometheus.CounterValue,
				float64(endpointStats.BytesReceived),
				endpointLabels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.BytesSent,
				prometheus.CounterValue,
				float64(endpointStats.BytesSent),
				endpointLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.PacketsReceived,
				prometheus.CounterValue,
				float64(endpointStats.PacketsReceived),
				endpointLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.PacketsSent,
				prometheus.CounterValue,
				float64(endpointStats.PacketsSent),
				endpointLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.DroppedPacketsIncoming,
				prometheus.CounterValue,
				float64(endpointStats.DroppedPacketsIncoming),
				endpointLabels...,
			)
			ch <- prometheus.MustNewConstMetric(
				c.DroppedPacketsOutgoing,
				prometheus.CounterValue,
				float64(endpointStats.DroppedPacketsOutgoing),
				endpointLabels...,
			)
		}
	}

	return nil, nil
}

func getContainerLabels(containerId string, owner string) []string {
	var details ContainerDetails
	var err error
	switch owner {
	case "containerd-shim-runhcs-v1.exe":
		details, err = getContainerInfoFromContainerD(containerId)
	default:
		// default to docker or if owner is not set
		details, err = getContainerInfoFromDocker(containerId)
	}

	if err != nil {
		details = ContainerDetails{
			ContainerName: "",
			Namespace:     "",
			PodName:       "",
			PodID:         "",
		}
	}

	var containerLabels []string
	if isKubernetes() {
		containerLabels = []string{
			details.ContainerId, details.ContainerName, details.Namespace, details.PodName, details.PodID,
		}
	} else {
		containerLabels = []string{
			details.ContainerId, details.ContainerName,
		}
	}

	return containerLabels

}

func getContainerInfoFromDocker(containerID string) (ContainerDetails, error) {
	// Create a new Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// Create a context for the operation
	ctx := context.Background()

	// Retrieve container details using the Docker client
	containerJSON, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("inspecting container: %w", err)
	}

	info := containerJSON.Config.Labels

	details := ContainerDetails{
		ContainerId:   "docker://" + containerID,
		ContainerName: info["io.kubernetes.container.name"],
		Namespace:     info["io.kubernetes.pod.namespace"],
		PodName:       info["io.kubernetes.pod.name"],
		PodID:         info["io.kubernetes.pod.uid"],
	}

	return details, nil
}

func getContainerInfoFromContainerD(containerID string) (ContainerDetails, error) {
	// Create a new client connected to the default socket path for containerd.
	client, err := containerd.New(`\\.\pipe\containerd-containerd`, containerd.WithDefaultNamespace("k8s.io"))
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("creating containerd client: %w", err)
	}
	defer client.Close()

	// Use the containerd namespace. This is typically "default" but could be
	// different if you've configured containerd differently.
	ctx := namespaces.WithNamespace(context.Background(), "k8s.io")

	// Get the container using the provided ID.
	container, err := client.LoadContainer(ctx, containerID)
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("loading container: %w", err)
	}

	info, _ := container.Info(ctx)

	details := ContainerDetails{
		ContainerId:   "containerd://" + containerID,
		ContainerName: info.Labels["io.kubernetes.container.name"],
		Namespace:     info.Labels["io.kubernetes.pod.namespace"],
		PodName:       info.Labels["io.kubernetes.pod.name"],
		PodID:         info.Labels["io.kubernetes.pod.uid"],
	}

	return details, nil
}
