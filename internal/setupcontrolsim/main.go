// setupcontrolsim serves registration-only CI fixtures. It never queues tasks.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/internal/worker"
	"github.com/Nischoy-ai/topo/internal/worker/controlsim"
	"golang.org/x/crypto/ssh"
)

func main() {
	dir := flag.String("dir", "", "private CI fixture directory")
	flag.Parse()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || os.Getenv("RUNNER_OS") != "Linux" || (os.Getenv("GITHUB_JOB") != "worker-setup-systemd" && os.Getenv("GITHUB_JOB") != "fedora-systemd") || *dir == "" {
		panic("disposable CI only")
	}
	must := func(err error) {
		if err != nil {
			panic("fixture initialization failed")
		}
	}
	write := func(name string, data []byte) { must(os.WriteFile(filepath.Join(*dir, name), data, 0600)) }
	token := make([]byte, 32)
	_, err := rand.Read(token)
	must(err)
	tokenText := hex.EncodeToString(token)
	write("token", []byte(tokenText))
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Topo disposable setup CI"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	must(err)
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	write("ca.crt", cert)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	must(err)
	write("key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	host, err := ssh.NewPublicKey(pub)
	must(err)
	write("known_hosts", append([]byte("127.0.0.2 "), ssh.MarshalAuthorizedKey(host)...))
	write("targets.allow", []byte("127.0.0.2/32\n"))
	sim := controlsim.New(controlsim.Config{Token: tokenText})
	var mu sync.Mutex
	counts := map[string]int{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "POST" {
			http.Error(w, "unexpected method", 405)
			return
		}
		suffix := strings.TrimPrefix(r.URL.Path, "/api/x_664635_topo/v1/tasks/")
		if suffix != "workers/register" && suffix != "workers/heartbeat" && suffix != "claim" {
			http.Error(w, "unexpected route", 403)
			return
		}
		body, e := io.ReadAll(io.LimitReader(r.Body, 65537))
		if e != nil || len(body) > 65536 {
			http.Error(w, "bounded request", 400)
			return
		}
		if suffix == "workers/register" {
			var reg worker.RegisterRequest
			if json.Unmarshal(body, &reg) != nil || reg.WorkerPool != "ci-setup" || reg.SiteID != "ci-site" {
				counts["rejected_pool"]++
				http.Error(w, "unknown pool", 403)
				return
			}
		}
		counts[suffix]++
		r.Body = io.NopCloser(bytes.NewReader(body))
		sim.Handler().ServeHTTP(w, r)
		snapshot := sim.Snapshot()
		audit := map[string]any{"counts": counts, "workers": len(snapshot.Workers), "runs": len(snapshot.Runs), "tasks": len(snapshot.Tasks), "deliveries": len(snapshot.Deliveries), "credentials": len(snapshot.CredentialAccesses)}
		out, e := json.Marshal(audit)
		if e != nil {
			panic("audit encoding")
		}
		write("audit.pending", out)
		must(os.Rename(filepath.Join(*dir, "audit.pending"), filepath.Join(*dir, "audit.json")))
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	must(err)
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second}
	write("origin", []byte("https://"+listener.Addr().String()))
	must(httpServer.ServeTLS(listener, filepath.Join(*dir, "ca.crt"), filepath.Join(*dir, "key.pem")))
}
