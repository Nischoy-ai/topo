package kubernetes

import (
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestMapNodesSkipsNodesWithoutUID(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: types.UID("uid-1")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2", UID: types.UID("")}},
	}
	inventories, byName := mapNodes(nodes, "cluster-a")
	if len(inventories) != 1 || inventories[0].NativeID != "uid-1" {
		t.Fatalf("got %#v", inventories)
	}
	if len(byName) != 1 || byName["cluster-a/node-1"] != "uid-1" {
		t.Fatalf("got name index %#v", byName)
	}
}

func TestMapNodesExtractsInfo(t *testing.T) {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: types.UID("uid-1")},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.5"}},
			NodeInfo:  corev1.NodeSystemInfo{OSImage: "Ubuntu 22.04", Architecture: "amd64"},
		},
	}
	inventories, _ := mapNodes([]corev1.Node{node}, "cluster-a")
	if len(inventories) != 1 {
		t.Fatalf("got %d nodes", len(inventories))
	}
	if inventories[0].OSImage != "Ubuntu 22.04" || len(inventories[0].Addresses) != 1 || inventories[0].Addresses[0] != "10.0.0.5" {
		t.Fatalf("got %#v", inventories[0])
	}
}

func TestMapPodsSkipsPodsWithoutUID(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("puid-1")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", UID: types.UID("")}},
	}
	inventories := mapPods(pods, "cluster-a", nil)
	if len(inventories) != 1 || inventories[0].NativeID != "puid-1" {
		t.Fatalf("got %#v", inventories)
	}
}

func TestMapPodsResolvesRunningNode(t *testing.T) {
	nodesByName := map[string]string{"cluster-a/node-1": "uid-1"}
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("puid-1")}, Spec: corev1.PodSpec{NodeName: "node-1"}},
	}
	inventories := mapPods(pods, "cluster-a", nodesByName)
	if len(inventories) != 1 || inventories[0].NodeNativeID != "uid-1" {
		t.Fatalf("got %#v", inventories)
	}
}

func TestMapPodsKeepsUnscheduledPodsWithoutNode(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("puid-1")}},
	}
	inventories := mapPods(pods, "cluster-a", nil)
	if len(inventories) != 1 || inventories[0].NodeNativeID != "" {
		t.Fatalf("got %#v", inventories)
	}
}

func TestInventoryAssetsProducesStableIdentityAndRelationships(t *testing.T) {
	inv := Inventory{
		Nodes: []NodeInventory{{NativeID: "uid-1", Name: "node-1"}},
		Pods:  []PodInventory{{NativeID: "puid-1", Name: "pod-1", Namespace: "default", NodeNativeID: "uid-1"}},
	}
	assets, relationships := inv.Assets(time.Now())
	if len(assets) != 2 {
		t.Fatalf("got %d assets, want 2 (node+pod)", len(assets))
	}
	for _, a := range assets {
		if a.Type != model.AssetKubernetesObject {
			t.Fatalf("got asset type %q, want %q", a.Type, model.AssetKubernetesObject)
		}
	}
	if len(relationships) != 1 || relationships[0].Type != "pod_runs_on_node" || relationships[0].FromNativeID != "puid-1" || relationships[0].ToNativeID != "uid-1" {
		t.Fatalf("got relationships %#v", relationships)
	}
}

func TestInventoryAssetsSkipsRelationshipForUnscheduledPod(t *testing.T) {
	inv := Inventory{Pods: []PodInventory{{NativeID: "puid-1", Name: "pod-1"}}}
	_, relationships := inv.Assets(time.Now())
	if len(relationships) != 0 {
		t.Fatalf("got %d relationships, want 0 for an unscheduled pod", len(relationships))
	}
}
