<a id="nischoy-topo-047-beta"></a>

# Nischoy Topo 0.4.7

Nischoy Topo lets ServiceNow control discovery of infrastructure that it
cannot reach directly. A stateless Topo worker connects outbound, discovers
only explicitly approved targets, and returns normalized observations for the
ServiceNow application to reconcile through IRE.

Topo 0.4.7 discovers Linux computers over SSH and Windows computers over WinRM
HTTPS, with their network interfaces, into the CMDB. See the
[tested scope](docs/evidence/mixed-inventory.md) for details and limits.

## Watch Topo 0.4.7

See Linux and Windows discovery, computer/interface inventory and CMDB
relationships in this narrated walkthrough. Turn on sound for narration.
Screenshots use time cuts.

https://github.com/user-attachments/assets/43a698e8-3988-4e2b-a118-0cba1dd3577c

## Download

Topo has two parts: the worker, which runs in your network, and the ServiceNow
app. Download both, or build them from source:
**[Get Topo: download or build](docs/get-topo.md)**.

| Part | Download |
| --- | --- |
| Worker for Linux | [x86-64](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_linux_amd64.tar.gz) · [ARM64](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_linux_arm64.tar.gz) · [.deb](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_amd64.deb) · [.rpm](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo-0.4.7-beta.1-1.x86_64.rpm) |
| Worker for macOS | [Apple silicon](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_darwin_arm64.tar.gz) · [Intel](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.1/topo_0.4.7-beta.1_darwin_amd64.tar.gz) |
| ServiceNow app | [XML with checklists (.zip)](https://github.com/Nischoy-ai/topo/releases/download/servicenow-0.4.7-beta.1/nischoy-topo-0.4.7-beta.zip) · [XML only](https://github.com/Nischoy-ai/topo/releases/download/servicenow-0.4.7-beta.1/nischoy-topo-0.4.7-combined.xml) |

## Get started

Start on a development instance with one Linux machine to discover. The
[first discovery run](docs/first-success.md) guide walks through each step:

1. [Install the ServiceNow app](docs/first-success.md#1-install-the-servicenow-app)
   by importing the XML.
2. [Add a worker user and your target](docs/first-success.md#2-set-up-access-and-five-records)
   in the app.
3. [Install and start the worker](docs/first-success.md#3-install-check-and-start-the-worker)
   on a Linux machine in your network.
4. [Click **Run now**](docs/first-success.md#4-run-inspect-and-repeat) and
   check the results in the CMDB.

Windows targets and subnets come after that; see
[Linux/Windows setup](docs/mixed-inventory-setup.md).

## Documentation and verification

| Need | Guide |
| --- | --- |
| Install and operate discovery | [Linux/Windows setup](docs/mixed-inventory-setup.md) · [Worker installation](docs/distribution.md) |
| Check the published package | [XML validation](docs/servicenow-validation.md) · [Release verification](docs/releases.md) |
| Review credential controls and report a vulnerability | [Deployment security](docs/deployment-security.md) · [Security policy](SECURITY.md) · [Security review record](docs/security-review.md) |
| Explore components or contribute | [Documentation index](docs/README.md) · [Developer quickstart](docs/development.md) · [Contributing](CONTRIBUTING.md) |
| See release scope, validation limits and plans | [Product roadmap](ROADMAP.md) |

Nischoy Topo is licensed under the [Apache License 2.0](LICENSE).
