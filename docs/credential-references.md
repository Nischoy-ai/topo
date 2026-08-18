# Credential references

Topo accepts credential provider references as configuration and resolves the
secret value only inside the process. A credential value must never be passed
as a CLI argument, job option, target, label, log attribute, or observation.

## Reference contract

The initial providers are:

- `env:NAME` reads the exact bytes of a non-empty environment variable. Names
  use portable ASCII environment-variable syntax.
- `file:/absolute/path` reads the exact bytes of a non-empty regular file.
  Paths must be absolute so startup behavior does not depend on the working
  directory. Symlinks to regular files are supported for mounted-secret
  rotation layouts.
- `vault:<path>#<field>` reads one field from a HashiCorp Vault KV version 2
  secret. See [Vault provider](#vault-provider) below.
- `k8s:[<namespace>/]<secret-name>#<field>` reads one field from a Kubernetes
  Secret. See [Kubernetes provider](#kubernetes-provider) below.

References are limited to 4 KiB and resolved values to 1 MiB. Consumers retain
their tighter validation, such as the WinRM password and controller API-key
limits. The resolver does not trim whitespace or trailing newlines because that
would silently change private keys and other credentials. Errors identify the
provider location when useful but never contain resolved credential bytes.

## CLI inputs

| Consumer | Default | Explicit flag |
| --- | --- | --- |
| Controller bearer key | optional `env:TOPO_API_KEY` | `-api-key-ref` |
| SSH password | optional `env:TOPO_SSH_PASSWORD` | `-password-ref` |
| SSH private key | none | `-private-key-ref` |
| WinRM password | required `env:TOPO_WINRM_PASSWORD` | `-password-ref` |

Existing `-password-env` and SSH `-private-key` flags remain deprecated aliases
for migration. Topo rejects use of an alias together with its replacement.

Examples:

```sh
./bin/topo serve -api-key-ref file:/run/secrets/topo_api_key

./bin/topo discover ssh \
  -targets targets.txt \
  -known-hosts /etc/topo/ssh_known_hosts \
  -private-key-ref file:/run/secrets/topo_ssh_key

./bin/topo discover winrm \
  -targets winrm-targets.txt \
  -username 'EXAMPLE\topo-reader' \
  -password-ref file:/run/secrets/topo_winrm_password \
  -auth ntlm
```

Restrict referenced files to the operating-system identity running Topo.
Environment variables are convenient for evaluation but can be inherited by
child processes and exposed by deployment tooling; restricted mounted files
are preferred for managed deployments.

A Kubernetes Secret mounted as a file can also use `file:`; this is filesystem
integration rather than the native `k8s:` API adapter, and both remain valid
depending on deployment constraints.

## Vault provider

`vault:<path>#<field>` reads one field from the latest version of a HashiCorp
Vault KV version 2 secret. `path` is the logical secret path inside the
configured mount, for example `topo/ssh`; `field` selects one key from the
secret's data map, for example `password`. Only KV version 2 is supported.

```sh
./bin/topo discover ssh \
  -targets targets.txt \
  -known-hosts /etc/topo/ssh_known_hosts \
  -password-ref vault:topo/ssh#password

./bin/topo discover winrm \
  -targets winrm-targets.txt \
  -username 'EXAMPLE\topo-reader' \
  -password-ref vault:topo/winrm#password \
  -auth ntlm
```

### Connection configuration

Connection settings are operator configuration read from the standard Vault
environment variables, not part of the reference itself, so one deployment
resolves every `vault:` reference against one Vault address and mount:

| Variable | Required | Purpose |
| --- | --- | --- |
| `VAULT_ADDR` | yes | Vault API base URL, `http://` or `https://`. |
| `VAULT_TOKEN` | one of `VAULT_TOKEN` / `VAULT_TOKEN_FILE` | Vault token value. |
| `VAULT_TOKEN_FILE` | one of `VAULT_TOKEN` / `VAULT_TOKEN_FILE` | Absolute path to a file containing the token, for Vault Agent or CSI driver token sinks. Preferred over `VAULT_TOKEN` in managed deployments. |
| `VAULT_NAMESPACE` | no | Vault Enterprise namespace. |
| `VAULT_MOUNT` | no | KV version 2 mount point. Defaults to `secret`. |
| `VAULT_CACERT` | no | Absolute path to an additional PEM certificate authority to trust. |

Topo verifies the Vault server's TLS identity using the system trust store
plus `VAULT_CACERT` when set; it never disables certificate verification.
Reads are bounded to 1 MiB and cancelled after 20 seconds. Errors report the
secret path and Vault's own error text but never resolved credential bytes.

### Token lease and renewal

Topo currently resolves each credential once at process startup and does not
hold a Vault client open across the resolved credential's lifetime, so a
single read only needs the configured token to be valid at that moment. Grant
the token least-privilege read access to the specific paths Topo needs and
prefer short discovery-run lifetimes over long-lived static tokens.

For deployments that keep a resolved credential's Vault session open longer,
the Vault client used internally exposes `LookupSelf` and `RenewSelf` so a
long-running consumer can renew the configured token before its lease
expires. Topo does not call these automatically today; automatic background
renewal for long-running processes, and support for leased dynamic secrets
engines beyond token renewal, remain deferred follow-ups.

## Kubernetes provider

`k8s:[<namespace>/]<secret-name>#<field>` reads one field from a Kubernetes
Secret's `data` map through the Kubernetes API server. `namespace` is
optional and defaults to the pod's own namespace; `secret-name` is the
Secret object's name; `field` selects one key, for example `password`.
Kubernetes always base64-encodes Secret data, and the client decodes it
before returning the credential bytes.

```sh
./bin/topo discover ssh \
  -targets targets.txt \
  -known-hosts /etc/topo/ssh_known_hosts \
  -password-ref k8s:ssh-credentials#password

./bin/topo discover winrm \
  -targets winrm-targets.txt \
  -username 'EXAMPLE\topo-reader' \
  -password-ref 'k8s:other-namespace/winrm-credentials#password' \
  -auth ntlm
```

### Connection configuration

Inside a pod, no configuration is required: Topo authenticates with the
pod's own projected service account token and trusts the cluster's own CA,
both mounted automatically by Kubernetes at the standard in-cluster paths.
The following environment variables override that default, primarily for
running outside a cluster (for example, local development or Topo Lab) or
for a non-default service account layout:

| Variable | Purpose |
| --- | --- |
| `TOPO_KUBERNETES_API_SERVER` | Overrides the API server URL normally built from `KUBERNETES_SERVICE_HOST`/`KUBERNETES_SERVICE_PORT`. When set, Topo trusts the system certificate store for TLS instead of the in-cluster CA unless `TOPO_KUBERNETES_CA_FILE` is also set. |
| `TOPO_KUBERNETES_TOKEN_FILE` | Absolute path to the bearer token file. Defaults to the projected service account token path. Re-read on every request, so kubelet-rotated tokens are picked up without any renewal logic. |
| `TOPO_KUBERNETES_CA_FILE` | Absolute path to an additional PEM certificate authority to trust for the API server's TLS identity. |
| `TOPO_KUBERNETES_NAMESPACE` | Default namespace used when a reference omits one. Defaults to the pod's own namespace file. |

Topo verifies the Kubernetes API server's TLS identity and never disables
certificate verification. Reads are bounded to 1 MiB and cancelled after 20
seconds. Errors report the secret name, namespace, and the Kubernetes API's
own error text but never resolved credential bytes.

### Least-privilege scoping

Kubernetes RBAC — not Topo — decides which Secrets a request may read. Grant
the pod's service account a `Role` with `get` (not `list` or `watch`) on the
specific named Secrets Topo needs, bound with a `RoleBinding` in that
namespace, rather than broad Secret read access:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: topo-credential-reader
  namespace: topo
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["ssh-credentials", "winrm-credentials"]
    verbs: ["get"]
```

A cross-namespace reference (`k8s:<namespace>/<secret-name>#<field>`) only
succeeds if the calling service account's RBAC grants also permit reading
Secrets in that other namespace.
