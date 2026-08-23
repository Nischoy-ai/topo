# Collector enrollment

Collector enrollment gives every Topo collector (today, the Topo Agent) its
own identity — a certificate — instead of every collector sharing the same
bearer API key. That certificate now also authenticates live traffic: the
controller can run a native mutual-TLS (mTLS) listener, and an enrolled
agent can present its certificate on collector data-plane requests instead
of, or alongside, the bearer API key. It does not authorize operator reads
or control-plane mutations. Since a collector certificate is
deliberately short-lived (90 days), it can also be renewed before it
expires, authenticated by itself rather than a new token, or revoked by
serial number before expiry after a suspected compromise. This is the
certificate lifecycle foundation used by the other collector capabilities —
[Collector heartbeats](heartbeats.md) and [job delivery](jobs.md) — are
separate capabilities that do not require enrollment or mTLS. See
[project plan](project-plan.md) for the full staged plan.

Enrollment and mTLS are entirely opt-in and additive. `topo serve` without
`-ca-dir` behaves exactly as it did before either slice existed, and
`-mtls` requires `-ca-dir` to be set.

## How it works

1. The controller acts as its own certificate authority. Starting
   `topo serve -ca-dir /path/to/ca` loads a CA persisted at that path, or
   generates and persists a new one (ECDSA P-256, self-signed, 10-year
   validity) if none exists yet.
2. An administrator selects the intended collector ID and mints a single-use,
   one-hour enrollment token bound to that exact identity using the
   controller's existing bearer API key, then gives it to the collector
   out-of-band (for example, baked into the collector's deployment
   configuration via the same credential-reference contract as every other
   Topo secret).
3. The collector generates its own ECDSA P-256 key pair locally and builds
   a CSR from it — **the private key never leaves the collector**; only the
   CSR (the public key plus a signature proving possession of the private
   key) is sent to the controller.
4. The controller validates the CSR's self-signature, rejects an enrollment
   identity that does not match the token without consuming it, then redeems
   the token and signs a 90-day client-authentication certificate for that
   identity. It returns the certificate along with the CA's own certificate.

```sh
# On the controller:
./bin/topo serve -api-key-ref env:TOPO_API_KEY -ca-dir /var/lib/topo-hub/ca
curl -s -X POST -H "Authorization: Bearer $TOPO_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"collector_id":"collector-1"}' \
  https://topo-hub.internal:8443/v1/enrollment-tokens
# {"token":"...","collector_id":"collector-1","expires_at":"..."}

# On the collector:
TOPO_AGENT_ENROLLMENT_TOKEN='<token from above>' ./bin/topo agent enroll \
  -controller-url https://topo-hub.internal:8443 \
  -collector-id collector-1 \
  -cert-dir /etc/topo-agent/enrollment
```

`topo agent enroll` writes `client-cert.pem`, `client-key.pem` (`0600`), and
`ca-cert.pem` to `-cert-dir` (`0700`). Pass that directory to
`topo agent run -mtls-cert-dir` to authenticate outbound requests with the
enrolled certificate — see [Running as native mTLS](#running-as-native-mtls)
below. Enrollment and rotation output includes the issued certificate's
canonical lowercase hexadecimal serial number; retain that output or recover
the value later with `openssl x509 -in client-cert.pem -noout -serial`.

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
| `-controller-url` | `topo agent rotate` | Controller base URL. Must be the `-mtls` listener — rotation has no bearer-key fallback. |
| `-cert-dir` | `topo agent rotate` | Absolute path holding the certificate, key, and CA certificate to renew; overwritten in place with the newly issued certificate. |

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
`tls.RequireAndVerifyClientCert`); application middleware enforces the
authorization policy per endpoint, not the TLS layer itself. A certificate
that *is* presented is still verified against the CA during the handshake,
and once verified it satisfies collector data-plane authorization without
needing the bearer key. Operator endpoints still require the bearer key — see
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

## Renewing a certificate

A collector certificate is deliberately short-lived (90 days,
`enrollment.DefaultCertificateTTL`), so it needs periodic renewal.
`topo agent rotate` does that, authenticated by the certificate it is
renewing rather than a new enrollment token:

```sh
./bin/topo agent rotate \
  -controller-url https://topo-hub.internal:8443 \
  -cert-dir /etc/topo-agent/enrollment
```

This presents the certificate currently in `-cert-dir` over mTLS to prove
identity, generates a fresh key pair and CSR (rotation renews the key, not
just the certificate), and overwrites `-cert-dir` with the newly issued
materials — the same layout `topo agent enroll` produces, so nothing else
about the collector's configuration needs to change.

Two things distinguish this from enrollment:

- **No bearer-API-key fallback.** `POST /v1/rotate` is unreachable without
  a client certificate the TLS handshake has already verified against the
  CA; unlike collector data-plane endpoints, presenting the correct bearer key
  instead does not work here. Accepting the shared bearer key would let
  anyone holding it mint a certificate for any collector ID, which defeats
  the entire point of per-collector identity — enrollment tokens are also
  collector-ID-scoped in the same spirit, just issued individually instead
  of shared.
- **The issued certificate's identity always matches the certificate
  presenting the request, never anything in the request body.** The
  controller reads the collector ID from the verified peer certificate's
  subject, not from the CSR; a CSR requesting a different common name is
  silently ignored rather than honored. A collector can only ever renew
  its own identity, never mint one for another collector.

Requires the controller to run `topo serve -mtls`: a certificate rotated
against a controller reachable only through a TLS-terminating reverse
proxy has no verified peer certificate to authenticate with, since the
proxy — not `topo serve` — terminates the connection. A certificate that
has already expired also cannot rotate itself this way (an expired
certificate fails the TLS handshake before `/v1/rotate` is ever reached);
re-enroll with a fresh token instead.

**`topo agent run` does not pick up a rotated certificate on its own.** It
loads the certificate once at startup; a collector running `agent run`
must be restarted after `agent rotate` for the new certificate to take
effect. Point a systemd timer or equivalent scheduler at
`topo agent rotate` well before the 90-day expiry, followed by a restart
of the `agent run` service, the same way a `certbot renew` timer is
typically paired with a reload of whatever consumes its certificate.

Rotation deliberately does not revoke the old serial automatically. This
avoids locking out a collector when the rotation response or subsequent file
write is lost. The command prints both old and new serials; after restarting
the agent and verifying the new certificate works, explicitly revoke the old
serial using the procedure below.

## Revoking and recovering a certificate

The operator can invalidate one exact collector certificate before its normal
expiry. Revocation is serial-specific: it does not disable the collector ID,
the CA, or a newly enrolled certificate for the same collector.

```sh
curl -s -X POST \
  -H "Authorization: Bearer $TOPO_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"serial_number":"<hex serial>","reason":"collector laptop stolen"}' \
  https://topo-hub.internal:8443/v1/certificate-revocations

curl -s -H "Authorization: Bearer $TOPO_API_KEY" \
  https://topo-hub.internal:8443/v1/certificate-revocations
```

`POST /v1/certificate-revocations` requires the operator bearer credential,
accepts common copied forms (`0x` prefix, uppercase, and colon separators),
and stores a canonical lowercase serial. The first request returns `201`; a
repeat for the same serial returns `200` with the original reason and
timestamp. Records are immutable and there is intentionally no "unrevoke"
endpoint. The authoritative record and a `certificate_revoked` audit event
include the reason but never certificate private-key material.

Application authorization checks the revocation repository after the TLS
handshake. A revoked certificate receives `401` on observation delivery,
heartbeats, job polling/results, and `POST /v1/rotate`; a repository lookup
failure returns `503` rather than accepting the credential. The TLS handshake
can still succeed because Topo does not publish a CRL or OCSP responder. This
enforcement therefore requires Topo's native `-mtls` listener to receive the
verified peer certificate; a reverse proxy that terminates mTLS without
forwarding a cryptographically trustworthy peer identity cannot provide this
boundary.

For compromise recovery:

1. Revoke the exposed serial using the operator bearer credential.
2. Mint a fresh single-use enrollment token.
3. Re-run `topo agent enroll` for the same collector ID. It creates a fresh
   key and a different serial; the old immutable revocation does not block it.
4. Restart `topo agent run` and verify heartbeats or delivery with the new
   certificate.

A revoked certificate cannot use rotation as a recovery path. Revocation and
rotation are linearized inside the single supported controller process: a
rotation already authorized completes before a competing revocation returns;
if the revocation wins first, rotation returns `401`. Serial values for both
certificates are returned and audited, so the operator can revoke the newly
issued serial too if incident timing shows that a rotation completed first.

Use `-db-driver sqlite` for an operational controller. Revocations then live
in schema version 4 and survive restarts. The default in-memory evaluation
backend implements identical request semantics but loses revocations on
restart, along with its other state; it is not a durable compromise boundary.
The separately possessed bearer key remains an independent compatibility
credential on collector endpoints, so revoke or rotate that key too if it may
also have been exposed.

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
- **Every token is bound to one collector ID when it is issued.** An
  enrollment request naming another identity receives the same generic error
  as an unknown, expired, or used token and does not consume the token. The
  intended collector can correct its configuration and retry once.
- **Tokens are single-use and in-memory.** The token store does not survive a
  restart even when discovery/audit/schedule persistence uses SQLite. Retry an
  in-flight enrollment with a freshly minted token after a controller
  restart.
- **Revocation is application-layer and serial-specific.** Topo does not
  publish a CRL/OCSP responder or revoke every certificate ever issued for a
  collector ID. This keeps recovery explicit and permits immediate
  re-enrollment with a fresh key while retaining immutable evidence for the
  compromised serial.
- **The controller's own server certificate is issued fresh on every
  `topo serve -mtls` start and is not persisted or rotated while the
  process runs.** Its 1-year TTL (`enrollment.DefaultServerCertificateTTL`)
  is deliberately longer than a collector certificate's 90 days for exactly
  this reason: it needs to outlive reasonably long controller uptimes
  rather than bound the blast radius of a single compromised key the way a
  collector certificate's TTL does. `topo agent rotate` renews a
  *collector's* certificate; it has no effect on the controller's own.
- **Rotation renews the key, not just the certificate.** Like enrollment, a
  fresh ECDSA P-256 key pair is generated locally for every rotation; the
  new certificate is never issued for the old public key. The old private
  key is simply discarded once `-cert-dir` is overwritten.
- **`-mtls` accepting connections with no client certificate
  (`tls.VerifyClientCertIfGiven`) is required for bootstrap, not a
  weakening of the trust model.** Enforcement still happens — every
  protected endpoint is still authorized in application middleware —
  collector endpoints accept a certificate verified against the CA or the
  bearer key, while operator endpoints require the bearer key — because
  the TLS layer has no way to make an exception for `POST /v1/enroll`.
- **Collector certificates are not administrator credentials.** They can
  deliver observations, send heartbeats, poll/report jobs, and rotate their
  own identity. Inventory/audit reads, collector-status reads, token issuance,
  job creation/status, and schedule operations require the configured bearer
  key. The bearer key remains accepted on collector endpoints for compatibility
  and still carries operator authority.

## Current limitations

- **Heartbeats and job delivery exist, but as independent capabilities
  not covered by this document** — see [Collector heartbeats](heartbeats.md)
  and [job delivery](jobs.md). Neither requires enrollment or mTLS.
- **No CRL/OCSP distribution or automatic old-serial revocation**, as above.
- **`-mtls-cert-dir` and `-api-key-ref` are independent flags on
  `topo agent run`; neither one currently causes the other to be skipped
  automatically.** Set only the credential you intend to use for a given
  collector.
- **Rotation is not automatic.** `topo agent rotate` must be invoked
  explicitly — by an operator, or a scheduler pointed at it — and a running
  `topo agent run` must be restarted afterward to pick up the new
  certificate. There is no in-process background renewal yet.
