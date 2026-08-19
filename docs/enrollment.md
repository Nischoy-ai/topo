# Collector enrollment

Collector enrollment gives every Topo collector (today, the Topo Agent) its
own identity — a certificate — instead of every collector sharing the same
bearer API key. That certificate now also authenticates live traffic: the
controller can run a native mutual-TLS (mTLS) listener, and an enrolled
agent can present its certificate on every outbound request instead of, or
alongside, the bearer API key. This is the first two of several slices
toward certificate rotation, heartbeats, and job delivery; see
[project plan](project-plan.md) for the full staged plan.

Enrollment and mTLS are entirely opt-in and additive. `topo serve` without
`-ca-dir` behaves exactly as it did before either slice existed, and
`-mtls` requires `-ca-dir` to be set.

## How it works

1. The controller acts as its own certificate authority. Starting
   `topo serve -ca-dir /path/to/ca` loads a CA persisted at that path, or
   generates and persists a new one (ECDSA P-256, self-signed, 10-year
   validity) if none exists yet.
2. An administrator mints a single-use, one-hour enrollment token using the
   controller's existing bearer API key, and gives it to the collector
   out-of-band (for example, baked into the collector's deployment
   configuration via the same credential-reference contract as every other
   Topo secret).
3. The collector generates its own ECDSA P-256 key pair locally and builds
   a CSR from it — **the private key never leaves the collector**; only the
   CSR (the public key plus a signature proving possession of the private
   key) is sent to the controller.
4. The controller validates the CSR's self-signature, then redeems the
   token, then signs a 90-day client-authentication certificate and returns
   it along with the CA's own certificate.

```sh
# On the controller:
./bin/topo serve -api-key-ref env:TOPO_API_KEY -ca-dir /var/lib/topo-hub/ca
curl -s -X POST -H "Authorization: Bearer $TOPO_API_KEY" \
  https://topo-hub.internal:8443/v1/enrollment-tokens
# {"token":"...","expires_at":"..."}

# On the collector:
TOPO_AGENT_ENROLLMENT_TOKEN='<token from above>' ./bin/topo agent enroll \
  -controller-url https://topo-hub.internal:8443 \
  -cert-dir /etc/topo-agent/enrollment
```

`topo agent enroll` writes `client-cert.pem`, `client-key.pem` (`0600`), and
`ca-cert.pem` to `-cert-dir` (`0700`). Pass that directory to
`topo agent run -mtls-cert-dir` to authenticate outbound requests with the
enrolled certificate — see [Running as native mTLS](#running-as-native-mtls)
below.

| Flag | Command | Purpose |
| --- | --- | --- |
| `-ca-dir` | `topo serve` | Absolute path to persist the enrollment CA. Enables `POST /v1/enrollment-tokens` and `POST /v1/enroll` when set. |
| `-mtls` | `topo serve` | Serve natively over mutual TLS, issuing the controller its own server certificate from `-ca-dir`'s CA and verifying client certificates against it. Requires `-ca-dir`. |
| `-mtls-san` | `topo serve` | Comma-separated DNS names/IPs for the controller's own TLS server certificate (default `localhost,127.0.0.1,::1`). |
| `-controller-url` | `topo agent enroll` | Controller base URL. |
| `-token-ref` | `topo agent enroll` | Credential reference for the one-time token (`env:`, `file:`, `vault:`, or `k8s:`); see [credential references](credential-references.md). |
| `-cert-dir` | `topo agent enroll` | Absolute path to write the issued certificate, private key, and CA certificate. |
| `-collector-id` | `topo agent enroll` | Requested certificate subject; defaults to the local hostname. |
| `-controller-ca-cert` | `topo agent enroll` | Absolute path to the controller's enrollment CA certificate. Required when the controller runs `topo serve -mtls`, since its TLS certificate is self-signed by that CA; distribute the file out-of-band alongside the enrollment token. Omit when TLS is terminated by a reverse proxy with a publicly-trusted certificate, or the controller runs over plain HTTP. |
| `-mtls-cert-dir` | `topo agent run` | Absolute path to the certificate, key, and CA certificate `topo agent enroll` wrote. Authenticates outbound requests with mutual TLS instead of, or alongside, `-api-key-ref`. |

## Running as native mTLS

`topo serve -mtls` issues the controller its own TLS server certificate
from the same CA that signs collector certificates, so a collector that
already trusts the CA (from enrollment) can verify the controller's TLS
identity without any separate certificate to manage:

```sh
# On the controller:
./bin/topo serve -api-key-ref env:TOPO_API_KEY -ca-dir /var/lib/topo-hub/ca \
  -mtls -mtls-san topo-hub.internal,127.0.0.1

# On the collector, after enrolling (note -controller-ca-cert — see below):
./bin/topo agent run \
  -controller-url https://topo-hub.internal:8443 \
  -mtls-cert-dir /etc/topo-agent/enrollment \
  -spool-dir /var/lib/topo-agent/spool -spool-key-ref env:TOPO_AGENT_SPOOL_KEY
```

A collector's very first request, `POST /v1/enroll`, has no certificate to
present yet — it authenticates with the one-time enrollment token instead.
The controller's mTLS listener therefore accepts a TLS handshake with no
client certificate at all (`tls.VerifyClientCertIfGiven`, not
`tls.RequireAndVerifyClientCert`); the `auth()` middleware enforces the
requirement per endpoint (a verified peer certificate or the bearer API
key), not the TLS layer itself. A certificate that *is* presented is still
verified against the CA during the handshake, and once verified it
satisfies `auth()` without needing the bearer key at all — see
[`internal/controller/server.go`](../internal/controller/server.go).

That same bootstrap step creates a chicken-and-egg problem for
`topo agent enroll` itself: the controller's TLS certificate under `-mtls`
is self-signed by its own enrollment CA, which system trust roots do not
recognize, so an ordinary HTTPS client cannot complete the enrollment
request at all. Pass `-controller-ca-cert` with the CA certificate
(distributed out-of-band, the same way the token already is — for example,
copied alongside it into the collector's deployment configuration) to pin
the connection to that specific CA for the enrollment request only. Omit it
when the controller's TLS is terminated by a reverse proxy with a
publicly-trusted certificate, or when enrolling over plain HTTP within a
trusted network.

## Design choices worth knowing

- **The CA's private key is protected by filesystem permissions (`0600`),
  not a second layer of application-level encryption.** This matches how
  every other private key in this project — SSH host/private keys, TLS
  keys in general — is conventionally protected. Encrypting it with another
  key that itself also needs protecting the same way does not add real
  security without something categorically stronger (a TPM, KMS, or HSM),
  which is out of scope here.
- **The token is redeemed only after the CSR passes structural validation
  and signature verification.** A malformed enrollment request never
  consumes a valid token, so an operator does not need to mint a new one
  just because a request had a typo.
- **Tokens are single-use and in-memory.** Like every other piece of
  controller state today, the token store does not survive a restart;
  persistent storage is a later, separately scoped milestone. Retry an
  in-flight enrollment with a freshly minted token after a controller
  restart.
- **No revocation.** A compromised collector certificate is contained by
  its bounded 90-day lifetime, not by a revocation list. If that proves
  insufficient, certificate rotation (the next slice) will need to
  reconsider it.
- **The controller's own server certificate is issued fresh on every
  `topo serve -mtls` start and is not persisted.** Its 1-year TTL
  (`enrollment.DefaultServerCertificateTTL`) is deliberately longer than a
  collector certificate's 90 days: unlike a collector certificate, it isn't
  renewed while the process keeps running, so it needs to outlive
  reasonably long controller uptimes rather than bound the blast radius of
  a single compromised key. Certificate rotation (the next slice) covers
  both.
- **`-mtls` accepting connections with no client certificate
  (`tls.VerifyClientCertIfGiven`) is required for bootstrap, not a
  weakening of the trust model.** Enforcement still happens — every
  protected endpoint requires either a certificate verified against the CA
  during the TLS handshake, or the bearer API key — it just happens in the
  `auth()` middleware instead of unconditionally in the TLS layer, because
  the TLS layer has no way to make an exception for `POST /v1/enroll`.

## Current limitations

- **No certificate rotation, heartbeats, or job delivery yet.** These are
  later slices of the same milestone.
- **No revocation**, as above.
- **`-mtls-cert-dir` and `-api-key-ref` are independent flags on
  `topo agent run`; neither one currently causes the other to be skipped
  automatically.** Set only the credential you intend to use for a given
  collector.
