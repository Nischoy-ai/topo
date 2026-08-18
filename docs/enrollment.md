# Collector enrollment

Collector enrollment gives every Topo collector (today, the Topo Agent) its
own identity — a certificate — instead of every collector sharing the same
bearer API key. This is the first of several slices toward outbound mTLS,
certificate rotation, heartbeats, and job delivery; see
[project plan](project-plan.md) for the full staged plan. This slice covers
enrollment itself: minting a token, submitting a certificate signing
request (CSR), and receiving a signed certificate. It does not yet change
how live traffic authenticates — that is the next slice.

Enrollment is entirely opt-in and additive. `topo serve` without `-ca-dir`
behaves exactly as it did before this slice existed.

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
`ca-cert.pem` to `-cert-dir` (`0700`). Nothing in this slice yet consumes
these files for live traffic; `topo agent run` still authenticates with the
bearer API key until the next slice wires the certificate into outbound
mTLS.

| Flag | Command | Purpose |
| --- | --- | --- |
| `-ca-dir` | `topo serve` | Absolute path to persist the enrollment CA. Enables `POST /v1/enrollment-tokens` and `POST /v1/enroll` when set. |
| `-controller-url` | `topo agent enroll` | Controller base URL. |
| `-token-ref` | `topo agent enroll` | Credential reference for the one-time token (`env:`, `file:`, `vault:`, or `k8s:`); see [credential references](credential-references.md). |
| `-cert-dir` | `topo agent enroll` | Absolute path to write the issued certificate, private key, and CA certificate. |
| `-collector-id` | `topo agent enroll` | Requested certificate subject; defaults to the local hostname. |

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
  insufficient, certificate rotation (the next-but-one slice) will need to
  reconsider it.

## Current limitations

- **No live mTLS yet.** The certificate this slice issues is not yet used
  to authenticate any live request; `topo serve` still runs as plain HTTP
  behind an operator-provided TLS-terminating reverse proxy, the same
  guidance as before. Wiring the enrolled certificate into outbound mTLS is
  the next slice.
- **No certificate rotation, heartbeats, or job delivery yet.** These are
  later slices of the same milestone.
- **No revocation**, as above.
