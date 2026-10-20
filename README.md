<a id="nischoy-topo-047-beta"></a>

# Nischoy Topo 0.4.7

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

## Watch Topo 0.4.7

See Linux and Windows discovery, computer/interface inventory and CMDB
relationships in this narrated walkthrough. Turn on sound for narration.
Screenshots use time cuts.

https://github.com/user-attachments/assets/43a698e8-3988-4e2b-a118-0cba1dd3577c

<a id="beta-scope"></a>

## Supported scope

This release supports ServiceNow-managed Linux computer/interface discovery over
verified SSH/22 and Windows computer/interface discovery over verified WinRM
HTTPS/5986 with NTLMv2. Separate protocol profiles select approved IPv4 addresses
or CIDRs with explicit exclusions, capped at 1,024 selected addresses per run.
Password2 credentials are released only for authorized task attempts; local
worker allowlists and trust files independently constrain access.

Workers are distributed for Linux and macOS. Real mixed managed acceptance uses
Linux workers; macOS mixed runtime is not independently exercised. Windows
worker/installer publication remains separate. See the [tested scope](docs/evidence/mixed-inventory.md).

CMDB publication supports only `cmdb_ci_computer`, `cmdb_ci_network_adapter`,
and `host_has_interface` mapped to `Owns::Owned by`. This release does not provide
the broad estate coverage needed to replace Device42, for either Linux or Windows estates. Other discovery components in the source tree have separate validation
limits; see the [roadmap](ROADMAP.md).

## Download

Topo has two parts: the worker, which runs in your network, and the ServiceNow
app. Download both, or build them from source:
**[Get Topo: download or build](docs/get-topo.md)**.

| Part | Download |
| --- | --- |
| Worker for Linux | [x86-64](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_linux_amd64.tar.gz) · [ARM64](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_linux_arm64.tar.gz) · [.deb](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_amd64.deb) · [.rpm](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo-0.4.7-beta.1-1.x86_64.rpm) |
| Worker for macOS | [Apple silicon](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_darwin_arm64.tar.gz) · [Intel](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_darwin_amd64.tar.gz) |
| ServiceNow app | [XML with checklists (.zip)](https://github.com/Nischoy-ai/topo/releases/download/servicenow-0.4.7-beta.1/nischoy-topo-0.4.7-beta.zip) · [XML only](https://github.com/Nischoy-ai/topo/releases/download/servicenow-0.4.7-beta.1/nischoy-topo-0.4.7-combined.xml) |

## Start ServiceNow discovery in three steps

Follow [your first ServiceNow discovery run](docs/first-success.md):

1. Verify and import the **Topo 0.4.7 native XML**; confirm all three sets,
   474 updates and 32 required ordered index definitions, then run the
   discovery-source Fix Script.
2. Configure least-privilege access, one approved Linux target and its verified
   host key; install the [signed 0.4.7 worker](docs/distribution.md), check it and start it.
3. Choose **Run now**, inspect the CMDB result and repeat to confirm
   reconciliation before enabling a schedule.

The guide starts with Linux SSH and adds Windows, CIDRs, the Linux background
service and macOS as optional next steps. Installation alone does not configure
or run discovery. Start on an authorized development instance and follow your
organization’s change process.

For full protocol/trust setup use [Linux/Windows setup](docs/mixed-inventory-setup.md).
The shipped Linux service enables SSH only. macOS provides a Homebrew CLI
formula; managed mixed runtime there remains unexercised.

The published combined XML was compared offline with reviewed native inputs.
Component restoration and index evidence are separately attributed; this is not
a fresh combined-byte or pristine-instance installation claim. Populated upgrade
testing is deferred, and independent unique enforcement tests were excluded;
neither is represented as passed. See the [validation record](docs/servicenow-validation.md).

## Documentation and verification

| Need | Guide |
| --- | --- |
| Install and operate discovery | [Linux/Windows setup](docs/mixed-inventory-setup.md) · [Worker installation](docs/distribution.md) |
| Check the published package | [XML validation](docs/servicenow-validation.md) · [Release verification](docs/releases.md) |
| Review credential controls and report a vulnerability | [Deployment security](docs/deployment-security.md) · [Security policy](SECURITY.md) · [Security review record](docs/security-review.md) |
| Explore components or contribute | [Documentation index](docs/README.md) · [Developer quickstart](docs/development.md) · [Contributing](CONTRIBUTING.md) |
| See release scope, validation limits and plans | [Product roadmap](ROADMAP.md) |

Nischoy Topo is licensed under the [Apache License 2.0](LICENSE).
