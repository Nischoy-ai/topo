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

A Kubernetes Secret mounted as a file can use `file:` today. This is filesystem
integration, not a native Kubernetes Secret API adapter. Native Kubernetes and
Vault providers, including their authentication, authorization, renewal, and
cancellation behavior, remain the next roadmap slice.
