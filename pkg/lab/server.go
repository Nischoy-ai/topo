package lab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Estate Estate
	mu     sync.Mutex
	calls  map[string]int
}

func NewServer(e Estate) *Server { return &Server{Estate: e, calls: map[string]int{}} }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/catalog", s.catalog)
	mux.HandleFunc("GET /v1/hosts/{id}/inventory", s.inventory)
	return mux
}
func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	items := make([]map[string]string, 0, len(s.Estate.Hosts))
	for _, h := range s.Estate.Hosts {
		items = append(items, map[string]string{"id": h.ID, "persona": h.Persona})
	}
	writeLabJSON(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var host *Host
	for i := range s.Estate.Hosts {
		if s.Estate.Hosts[i].ID == id {
			host = &s.Estate.Hosts[i]
			break
		}
	}
	if host == nil {
		http.Error(w, "not found", 404)
		return
	}
	s.mu.Lock()
	s.calls[id]++
	calls := s.calls[id]
	s.mu.Unlock()
	if host.LatencyMS > 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Duration(host.LatencyMS) * time.Millisecond):
		}
	}
	switch host.Fault {
	case "auth_failure":
		http.Error(w, "simulated authentication failure", 401)
		return
	case "timeout":
		<-r.Context().Done()
		return
	case "malformed":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"broken":`))
		return
	case "disappear":
		if calls > 1 {
			http.Error(w, "simulated host disappeared", 410)
			return
		}
	}
	out := *host
	if host.Fault == "permission_denied" {
		out.Packages = nil
		out.Services = nil
		w.Header().Set("X-Topo-Lab-Partial", "permission_denied")
	}
	writeLabJSON(w, 200, out)
}
func writeLabJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Topo-Lab-Protocol", ScenarioVersion)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func ParseListenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return fmt.Sprintf("http://%s", addr)
}
