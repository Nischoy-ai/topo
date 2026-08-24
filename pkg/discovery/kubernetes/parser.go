package kubernetes

import (
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	corev1 "k8s.io/api/core/v1"
)

type NodeInventory struct {
	NativeID    string
	Name        string
	Addresses   []string
	OSImage     string
	KernelVer   string
	Runtime     string
	Arch        string
	CPUCapacity string
	MemCapacity string
}

type PodInventory struct {
	NativeID     string
	Name         string
	Namespace    string
	Phase        string
	Addresses    []string
	NodeNativeID string
}

type Inventory struct {
	Nodes []NodeInventory
	Pods  []PodInventory
}

// mapNodes maps raw v1.Node objects to Topo's node inventory, keyed by the
// node's Kubernetes UID rather than its name — a name is reusable (a node
// can be deleted and a differently-provisioned machine later registered
// under the same name) while a UID is not. clusterTag namespaces the
// returned name index so mapPods can resolve a pod's node across a
// multi-target scan without two different clusters' identically-named
// nodes colliding.
func mapNodes(nodes []corev1.Node, clusterTag string) ([]NodeInventory, map[string]string) {
	inventories := make([]NodeInventory, 0, len(nodes))
	byName := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nativeID := string(node.UID)
		if nativeID == "" {
			continue
		}
		var addresses []string
		for _, addr := range node.Status.Addresses {
			addresses = append(addresses, addr.Address)
		}
		inventories = append(inventories, NodeInventory{
			NativeID:    nativeID,
			Name:        node.Name,
			Addresses:   addresses,
			OSImage:     node.Status.NodeInfo.OSImage,
			KernelVer:   node.Status.NodeInfo.KernelVersion,
			Runtime:     node.Status.NodeInfo.ContainerRuntimeVersion,
			Arch:        node.Status.NodeInfo.Architecture,
			CPUCapacity: node.Status.Capacity.Cpu().String(),
			MemCapacity: node.Status.Capacity.Memory().String(),
		})
		byName[clusterTag+"/"+node.Name] = nativeID
	}
	return inventories, byName
}

// mapPods maps raw v1.Pod objects to Topo's pod inventory. A pod missing a
// UID is skipped, for the same reason a node missing one is skipped in
// mapNodes; an unscheduled pod (no NodeName yet) is kept, just without a
// resolved NodeNativeID, since PodPending is a legitimate transient state,
// not a parse failure.
func mapPods(pods []corev1.Pod, clusterTag string, nodesByName map[string]string) []PodInventory {
	inventories := make([]PodInventory, 0, len(pods))
	for _, pod := range pods {
		nativeID := string(pod.UID)
		if nativeID == "" {
			continue
		}
		var addresses []string
		for _, addr := range pod.Status.PodIPs {
			addresses = append(addresses, addr.IP)
		}
		inventories = append(inventories, PodInventory{
			NativeID:     nativeID,
			Name:         pod.Name,
			Namespace:    pod.Namespace,
			Phase:        string(pod.Status.Phase),
			Addresses:    addresses,
			NodeNativeID: nodesByName[clusterTag+"/"+pod.Spec.NodeName],
		})
	}
	return inventories
}

func (inv Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "kubernetes-cluster", Collected: now, Confidence: 1}}
	var assets []model.Asset
	var relationships []model.Relationship

	for _, node := range inv.Nodes {
		assets = append(assets, model.Asset{
			Type:     model.AssetKubernetesObject,
			NativeID: node.NativeID,
			Name:     node.Name,
			Identifiers: map[string]string{
				"kind": "Node",
				"uid":  node.NativeID,
			},
			Attributes: map[string]any{
				"kind":              "Node",
				"addresses":         node.Addresses,
				"os_image":          node.OSImage,
				"kernel_version":    node.KernelVer,
				"container_runtime": node.Runtime,
				"architecture":      node.Arch,
				"cpu_capacity":      node.CPUCapacity,
				"memory_capacity":   node.MemCapacity,
			},
			Evidence: ev,
		})
	}

	for _, pod := range inv.Pods {
		assets = append(assets, model.Asset{
			Type:     model.AssetKubernetesObject,
			NativeID: pod.NativeID,
			Name:     pod.Name,
			Identifiers: map[string]string{
				"kind":      "Pod",
				"uid":       pod.NativeID,
				"namespace": pod.Namespace,
			},
			Attributes: map[string]any{
				"kind":      "Pod",
				"namespace": pod.Namespace,
				"phase":     pod.Phase,
				"addresses": pod.Addresses,
			},
			Evidence: ev,
		})
		if pod.NodeNativeID != "" {
			relationships = append(relationships, model.Relationship{Type: "pod_runs_on_node", FromNativeID: pod.NativeID, ToNativeID: pod.NodeNativeID, Evidence: ev})
		}
	}

	return assets, relationships
}
