# Mixed inventory candidate evidence

These results concern the unpublished managed mixed-inventory source candidate,
not the published 0.4.6 app or `v0.4.6-beta.1` worker. The full acceptance plan is
[managed mixed inventory](../mixed-inventory-pilot.md).

## Real collection — 2026-10-10

An existing disposable Azure lab supplied Ubuntu 24.04 and Windows Server 2022
and 2025. Target machines have no public IP. Windows scan accounts are
non-admin, with Remote Management Users and read access to WMI `root/cimv2`.
WinRM permits HTTPS/5986 and NTLMv2; Basic/unencrypted authentication are off.
The certificate trust was established through the Azure control plane before
this test. Candidate binaries ran from disposable paths, leaving the installed
Topo and service configuration intact. Credentials were streamed over verified
SSH and resolved from environment references; summaries contain no secrets.

| Target | Mode | Repeat result |
| --- | --- | --- |
| Ubuntu 24.04 | Production SSH worker executor with verified host keys | 3 assets, 2 relationships per scan; zero errors; identical identities across two scans; offline IRE preview passed |
| Windows Server 2022 | Production worker executor; fixed computer/interface mode | 2 assets, 1 relationship per scan; zero errors; identical identities across two scans; offline IRE preview passed |
| Windows Server 2025 | Production worker executor; fixed computer/interface mode | 2 assets, 1 relationship per scan; zero errors; identical identities across two scans; offline IRE preview passed |

The original lab run exposed two compatibility failures. WQL enumeration used
a class-specific resource URI instead of the documented namespace wildcard;
NTLM transport disabled the HTTP/2 handler but retained inherited `h2` ALPN.
The candidate uses the exact reviewed WQL with the wildcard URI and explicitly
advertises HTTP/1.1 for NTLM. Both fixes have regression tests. Microsoft's
[WQL enumeration example](https://learn.microsoft.com/en-us/windows/win32/winrm/querying-for-specific-instances-of-a-resource)
provides the URI contract; arbitrary WQL remains rejected by the fixture.

Direct scans and offline preview establish collection and mapping compatibility
on these two machines. They do not prove the real ServiceNow broker, ACLs,
transaction behavior, IRE reconciliation, app install/upgrade or a general
Windows compatibility guarantee. Address-change and domain/Kerberos behavior
were not exercised. Broader standalone disk/service/patch/software inventory
was deliberately excluded; no privileges were added to enable a WinRS shell.

Immutable candidate commit/build attribution and final verification results
will be added after the reviewable source is committed. Managed acceptance and
candidate publication remain open.
