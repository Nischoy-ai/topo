package lab

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
)

// LabKubernetesToken is the fixed bearer token the Topo Lab Kubernetes
// server accepts. It is not a real credential — Topo Lab is loopback-only
// and every other Lab fixture (SSH, WinRM, SNMP) similarly fixes its
// accepted credential rather than generating one per run.
const LabKubernetesToken = "topo-lab-token"

// KubernetesServer exposes one simulated Kubernetes cluster — one Node and
// one Pod per Estate host — over the real Kubernetes REST API JSON shape
// (k8s.io/api/core/v1 types, encoded the same way a real API server would),
// covering only the endpoints the production plugin actually calls:
// GET /version, GET /api/v1/nodes, GET /api/v1/pods. It is not a general
// Kubernetes API server implementation — no watch, no other resource,
// no write verb — the same scoped-fixture posture as the SNMP Lab agent.
type KubernetesServer struct {
	Estate Estate
}

func NewKubernetesServer(estate Estate) *KubernetesServer {
	return &KubernetesServer{Estate: estate}
}

func (server *KubernetesServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", server.handleVersion)
	mux.HandleFunc("GET /api/v1/nodes", server.authenticated(server.handleNodes))
	mux.HandleFunc("GET /api/v1/pods", server.authenticated(server.handlePods))
	return mux
}

// authenticated enforces the same bearer-token contract a real API server
// would: a missing or wrong token is rejected before any inventory is
// returned, exercised by TestKubernetesWrongTokenIsIsolatedAsConnectError.
func (server *KubernetesServer) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+LabKubernetesToken {
			writeStatus(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next(w, r)
	}
}

func (server *KubernetesServer) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Info{Major: "1", Minor: "31", GitVersion: "v1.31.0+topo-lab"})
}

func (server *KubernetesServer) handleNodes(w http.ResponseWriter, _ *http.Request) {
	items := make([]corev1.Node, 0, len(server.Estate.Hosts))
	for _, host := range server.Estate.Hosts {
		items = append(items, corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: host.Name,
				UID:  types.UID(host.ID),
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: host.IPAddress}},
				NodeInfo: corev1.NodeSystemInfo{
					OSImage:                 host.OS + " " + host.OSVersion,
					KernelVersion:           "topo-lab-kernel",
					ContainerRuntimeVersion: "containerd://topo-lab",
					Architecture:            host.Architecture,
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", max(host.CPUCount, 1))),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", max(host.MemoryMB, 1))),
				},
			},
		})
	}
	writeJSON(w, http.StatusOK, corev1.NodeList{Items: items})
}

func (server *KubernetesServer) handlePods(w http.ResponseWriter, _ *http.Request) {
	items := make([]corev1.Pod, 0, len(server.Estate.Hosts))
	for _, host := range server.Estate.Hosts {
		podIPBytes, _ := hex.DecodeString(stableToken(server.Estate.Scenario.Seed, "pod-ip", host.ID)[:4])
		items = append(items, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      host.Name + "-workload",
				Namespace: "default",
				UID:       types.UID(stableToken(server.Estate.Scenario.Seed, "pod-uid", host.ID)),
			},
			Spec: corev1.PodSpec{NodeName: host.Name},
			Status: corev1.PodStatus{
				Phase:  corev1.PodRunning,
				PodIPs: []corev1.PodIP{{IP: fmt.Sprintf("10.244.%d.%d", podIPBytes[0], podIPBytes[1])}},
			},
		})
	}
	writeJSON(w, http.StatusOK, corev1.PodList{Items: items})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeStatus mirrors the shape of a real Kubernetes API error response
// (a meta/v1 Status object) closely enough for client-go's own error
// decoding to recognize it as an API error rather than a malformed body.
func writeStatus(w http.ResponseWriter, code int, reason string) {
	writeJSON(w, code, metav1.Status{
		Status:  metav1.StatusFailure,
		Message: reason,
		Reason:  metav1.StatusReason(reason),
		Code:    int32(code),
	})
}
