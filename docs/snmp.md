# SNMP network device discovery

Topo's SNMP discovery slice collects stable device identity and interface inventory from network equipment over SNMPv3, using only the MIB-II `system` and `interfaces` groups. This is the first slice of the SNMP/VMware discovery milestone; SNMPv1/v2c, vendor-specific MIBs, and topology protocols (LLDP/CDP) are deliberately out of scope for this slice, not silently unsupported forever.

## Audited operation contract

The plugin queries a fixed, compiled-in set of OIDs; a discovery request supplies target addresses only, never an OID, community string, or arbitrary SNMP operation.

| OID | MIB-II object | Required | Normalized data |
| --- | --- | --- | --- |
| `1.3.6.1.2.1.1.1.0` | `sysDescr` | Yes | device description attribute |
| `1.3.6.1.2.1.1.2.0` | `sysObjectID` | No | vendor/model object identifier attribute |
| `1.3.6.1.2.1.1.3.0` | `sysUpTime` | No | uptime ticks attribute |
| `1.3.6.1.2.1.1.5.0` | `sysName` | Yes | host asset name |
| `1.3.6.1.2.1.2.2.1.2` (`ifDescr`) | interfaces table | No | interface name |
| `1.3.6.1.2.1.2.2.1.6` (`ifPhysAddress`) | interfaces table | No | interface MAC address |

`sysName` is required: a device that cannot answer it fails discovery for that target with a `snmp_operation` error rather than emitting an unnamed asset. The interfaces-table walk (`ifDescr`/`ifPhysAddress`, via SNMP GETBULK) is optional: a walk failure emits a retryable `snmp_partial` error and omits interface assets for that target, but does not fail the host. Asset identity is the SNMPv3 engine ID discovered during the USM handshake (hex-encoded), not an IP address — engine IDs are stable device identities per RFC 3411 §5, the SNMP analog of the machine ID Topo's SSH plugin and the UUID its WinRM plugin already use for the same purpose.

## Security level and transport

Production discovery requires SNMPv3 `authPriv` with SHA authentication and AES privacy — the only security level and protocol pair this plugin accepts outside Topo Lab, mirroring how the WinRM plugin's production path requires NTLM+HTTPS and permits no weaker fallback. `noAuthNoPriv` is accepted only with `-lab` (Topo Lab loopback targets); `authNoPriv` is not supported at all, since a device configured to allow it is not meaningfully more resistant to tampering than one skipped straight to `authPriv`. Authentication and privacy passphrases must be at least 8 characters, matching RFC 3414's minimum, and resolve through the same bounded, non-CLI credential-reference contract as every other Topo credential.

```sh
TOPO_SNMP_AUTH_PASSPHRASE_REF=env:SNMP_AUTH_PASSPHRASE \
TOPO_SNMP_PRIV_PASSPHRASE_REF=env:SNMP_PRIV_PASSPHRASE \
./bin/topo discover snmp \
  -targets snmp-targets.txt \
  -site pilot \
  -username topo-reader \
  -auth-passphrase-ref env:SNMP_AUTH_PASSPHRASE \
  -priv-passphrase-ref env:SNMP_PRIV_PASSPHRASE
```

`-auth-protocol` and `-priv-protocol` default to `SHA` and `AES`; any other value is rejected outside Topo Lab. See [credential references](credential-references.md) for the full provider list (`env:`, `file:`, `vault:`, `k8s:`).

## Dependency

The plugin uses [`github.com/gosnmp/gosnmp`](https://github.com/gosnmp/gosnmp), the ecosystem-standard pure-Go SNMP client, pinned to `v1.42.1` — the last release declaring `go 1.22` compatibility with this project's pinned `go 1.23.0` toolchain; newer releases require `go 1.24` and were deliberately not used to avoid silently bumping the language version CI is pinned to. Hand-rolling SNMP's BER/ASN.1 wire format and SNMPv3 USM's authentication/privacy crypto (RFC 3414/3826) from scratch is exactly the kind of well-trodden, security-sensitive protocol work this project's "prefer standard-library components and narrowly scoped dependencies" principle exists to weigh against, not to forbid outright — this is the project's third external dependency, after `golang.org/x/crypto` (SSH) and `github.com/Azure/go-ntlmssp` (WinRM NTLM), each added for the same reason.

## Topo Lab usage

Topo Lab's SNMP agent is hand-rolled rather than reused from gosnmp, since gosnmp is client-only and there is no off-the-shelf SNMP agent simulator comparable to govmomi's `vcsim`. It answers `noAuthNoPriv` only — implementing USM's authentication/privacy crypto on the server side is out of scope for a test fixture — but it is not a simplified stand-in for the wire protocol itself: it decodes requests and encodes responses using gosnmp's own exported `SnmpDecodePacket`/`SnmpPacket.MarshalMsg`, including a real SNMPv3 engine ID discovery handshake, so the acceptance test exercises the plugin's actual message framing. `authPriv` is implemented via gosnmp's own client-side USM implementation, but — like WinRM real-host fixtures before it — is implemented and unit-verified, not verified against a real device; do not represent it as validated against real network equipment.

Unlike the SSH and WinRM Lab servers, which multiplex simulated hosts behind one listener by username, the SNMP Lab server binds one loopback UDP socket per simulated host, each on an OS-assigned ephemeral port — closer to how real network equipment is deployed (one address per device) than a shared-listener/username scheme would be. Because addresses are not fixed by a flag, serving and listing targets are combined into a single command:

```sh
make build
./bin/topo lab snmp-serve \
  -scenario examples/lab/clean-500.json > snmp-targets.txt
```

`snmp-targets.txt` receives one `host:port` line per simulated host on stdout while the command blocks, serving. In another terminal:

```sh
./bin/topo discover snmp \
  -targets snmp-targets.txt \
  -site lab \
  -lab
```

`-lab` selects `noAuthNoPriv` and defaults the username to `topo-lab` (not enforced by the Lab agent, since `noAuthNoPriv` performs no authentication — any username is accepted, exactly as a real `noAuthNoPriv` agent would behave). Every Topo Lab target must resolve syntactically to `localhost` or a loopback address; this is enforced the same way as SSH and WinRM Lab mode.

Each simulated host answers with one interface (`primary`, matching the "primary" interface convention the SSH and WinRM Lab fixtures also use) and reports `sysUpTime` from wall-clock time, so it changes between scans even though the engine-ID-derived asset identity does not — the same "value changes, identity does not" property this project's ServiceNow validation work already confirmed matters for real reconciliation.

## Security and transport behavior

- Production targets must use `authPriv` with SHA authentication and AES privacy; there is no fallback to a weaker security level or protocol.
- Request options whose names indicate passwords, passphrases, credentials, tokens, or secrets are rejected.
- The username and passphrases are bounded and checked for control characters; passphrases never appear in CLI arguments, only through credential references.
- Interface table walks are bounded to 4096 entries so a malformed or hostile agent cannot force unbounded memory use.
- Target concurrency is bounded and cancellation propagates through the underlying SNMP requests.
- Structured errors include the target and failing operation, never credentials.

## Current limitations and next slice

This slice covers MIB-II `system` and `interfaces` only — no vendor MIBs, no LLDP/CDP topology, no SNMPv1/v2c. `authPriv` is implemented but not yet verified against a real device; Topo Lab's `noAuthNoPriv`-only agent proves the plugin's own parsing and mapping logic, not interoperability with real network equipment. VMware vCenter discovery is the next slice in this milestone; see `docs/project-plan.md` for the full spec.
