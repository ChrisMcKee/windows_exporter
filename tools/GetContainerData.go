package main

import (
	"context"
	"fmt"
	"github.com/Microsoft/hcsshim"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/docker/docker/client"
	"golang.org/x/sys/windows/svc/mgr"
)

func main() {
	containers, err := hcsshim.GetContainers(hcsshim.ComputeSystemQuery{Types: []string{"Container"}})
	if err != nil {
		print(fmt.Errorf("failed to get containers"))
		return
	}

	count := len(containers)

	if count == 0 {
		print("No containers found")
		return
	}

	for _, containerDetails := range containers {
		container, err := hcsshim.OpenContainer(containerDetails.ID)
		if container != nil {
			//defer c.containerClose(container)
		}
		if err != nil {
			print("err in opening container", "containerId", containerDetails.ID)
			continue
		}

		if checkService("docker") {
			GetDocContainerDetails(containerDetails.ID)
		}
		if checkService("containerd") {
			GetContainerDetails(containerDetails.ID)
		}

	}
}

func GetDocContainerDetails(containerID string) error {
	// Create a new Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// Create a context for the operation
	ctx := context.Background()

	// Retrieve container details using the Docker client
	containerJSON, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspecting container: %w", err)
	}

	// Print some details about the container
	fmt.Printf("Container ID: %s\n", containerJSON.ID)
	fmt.Printf("Image: %s\n", containerJSON.Image)
	fmt.Printf("Command: %s\n", containerJSON.Config.Cmd)
	fmt.Printf("State: %s\n", containerJSON.State.Status)
	fmt.Printf("Created: %s\n", containerJSON.Created)
	for s, s2 := range containerJSON.Config.Labels {
		fmt.Printf("Label: %s:%s\n", s, s2)
	}

	return nil
}

type ContainerDetails struct {
	ContainerId string
	Image       string
	Created     string
	Namespace   string
	PodName     string
}

func GetContainerDetails(containerID string) error {
	// Create a new client connected to the default socket path for containerd.
	client, err := containerd.New(`\\.\pipe\containerd-containerd`, containerd.WithDefaultNamespace("k8s.io"))
	if err != nil {
		return fmt.Errorf("creating containerd client: %w", err)
	}
	defer client.Close()

	// Use the containerd namespace. This is typically "default" but could be
	// different if you've configured containerd differently.
	ctx := namespaces.WithNamespace(context.Background(), "k8s.io")

	// Get the container using the provided ID.
	container, err := client.LoadContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("loading container: %w", err)
	}

	// Get the container's task, which represents the main process running
	// inside the container.
	task, err := container.Task(ctx, nil)
	if err != nil {
		return fmt.Errorf("getting container task: %w", err)
	}

	// Get the status of the task.
	status, err := task.Status(ctx)
	if err != nil {
		return fmt.Errorf("getting task status: %w", err)
	}

	// Print some details about the container.
	fmt.Printf("Container ID: %s\n", container.ID())
	fmt.Printf("Task PID: %d\n", task.Pid())
	fmt.Printf("Status: %s\n", status.Status)

	labels, _ := container.Labels(ctx)
	for s, s2 := range labels {
		fmt.Printf("Label: %s:%s\n", s, s2)
	}

	return nil
}

func checkService(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Printf("Failed to connect to service manager: %v\n", err)
		return false
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		fmt.Printf("%s is not installed.\n", name)
		return false
	}
	defer s.Close()

	q, err := s.Query()
	if err != nil {
		fmt.Printf("Failed to query service status: %v\n", err)
		return false
	}

	fmt.Printf("%s is %s.\n", name, serviceState(uint32(q.State)))
	return true
}

func serviceState(state uint32) string {
	switch state {
	case 1:
		return "stopped"
	case 4:
		return "running"
	default:
		return "unknown"
	}
}
