# Get Topo: download or build

Topo has two parts, and you need both:

- **The worker** (`topo`): a single program that runs on a Linux or macOS
  machine inside your network and discovers your hosts.
- **The ServiceNow app**: installed once into your ServiceNow instance. It
  holds your targets, credentials and schedules, and receives the results.

For each part you can download what we publish, or build it yourself from this
repository. Downloading is the quickest way to start.

Current version: **0.4.7 beta**.

## The worker

### Download

| Platform | Download |
| --- | --- |
| Linux x86-64 | [topo_0.4.7-beta.2_linux_amd64.tar.gz](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_linux_amd64.tar.gz) |
| Linux ARM64 | [topo_0.4.7-beta.2_linux_arm64.tar.gz](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_linux_arm64.tar.gz) |
| macOS Intel | [topo_0.4.7-beta.2_darwin_amd64.tar.gz](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_darwin_amd64.tar.gz) |
| macOS Apple silicon | [topo_0.4.7-beta.2_darwin_arm64.tar.gz](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_darwin_arm64.tar.gz) |
| Debian / Ubuntu package | [amd64 .deb](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_amd64.deb) · [arm64 .deb](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo_0.4.7-beta.2_arm64.deb) |
| Fedora / RHEL package | [x86_64 .rpm](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo-0.4.7-beta.2-1.x86_64.rpm) · [aarch64 .rpm](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/topo-0.4.7-beta.2-1.aarch64.rpm) |

All files, including [`SHA256SUMS`](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/SHA256SUMS),
are on the [v0.4.7-beta.2 release page](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.7-beta.2).

Download `SHA256SUMS` next to your file, check it, then unpack:

```sh
grep linux_amd64.tar.gz SHA256SUMS | sha256sum -c   # macOS: use shasum -a 256 -c
tar xzf topo_0.4.7-beta.2_linux_amd64.tar.gz
sudo install topo_0.4.7-beta.2_linux_amd64/topo /usr/local/bin/topo
topo version                                  # prints v0.4.7-beta.2
```

The `.deb` and `.rpm` packages also add systemd units for running the worker
as a background service. Install them with the package manager so it handles
dependencies:

```sh
sudo apt install ./topo_0.4.7-beta.2_amd64.deb     # Debian / Ubuntu
sudo dnf install ./topo-0.4.7-beta.2-1.x86_64.rpm  # Fedora / RHEL
```

Installing does not start anything. To get updates through `apt` or `dnf`
instead, use the [signed package repositories](distribution.md). For signature
checks beyond the checksum, see [release verification](releases.md#verify-a-downloaded-release).

### Build from source

You need Go 1.26 or later and Git.

```sh
git clone https://github.com/Nischoy-ai/topo.git
cd topo
git checkout v0.4.7-beta.2
make build
./bin/topo version
```

`make build` writes `bin/topo` for the machine you are on. To build for
another platform, set `GOOS` and `GOARCH`:

```sh
GOOS=linux GOARCH=arm64 go build -trimpath -o topo-linux-arm64 ./cmd/topo
```

The build downloads Go modules from the Go module proxy. If your network
blocks it, set `GOPROXY` to your internal mirror.

## The ServiceNow app

### Download

The app is on the same [v0.4.7-beta.2 release page](https://github.com/Nischoy-ai/topo/releases/tag/v0.4.7-beta.2):

- [nischoy-topo-0.4.7-beta.zip](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/nischoy-topo-0.4.7-beta.zip):
  the XML plus the installation checklists, which is what most people want
- [nischoy-topo-0.4.7-combined.xml](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/nischoy-topo-0.4.7-combined.xml):
  the XML on its own
- [SHA256SUMS](https://github.com/Nischoy-ai/topo/releases/download/v0.4.7-beta.2/SHA256SUMS)

Import the XML through **Retrieved Update Sets > Import Update Set from XML**,
then preview and commit it. Follow the
[installation checklist](servicenow-update-set.md#customer-installation): it
lists the counts to expect and one fix script to run after the commit.

This XML is the installation path we have tested.

### Build from source

The app is written with the ServiceNow SDK and can be installed straight into
your instance from this repository. Use a development instance.

You need Node.js 20 and npm, admin access to the instance, and a ServiceNow SDK
authentication alias for that instance. See the ServiceNow SDK documentation
for creating the alias.

1. Build and install the app:

   ```sh
   git clone https://github.com/Nischoy-ai/topo.git
   cd topo
   git checkout v0.4.7-beta.2
   scripts/install-servicenow-app.sh <your-sdk-alias>
   ```

   The script installs the SDK, runs the app's tests, builds the app and
   installs it with `now-sdk install`.

2. Check the database indexes against the
   [32-index checklist](servicenow-index-setup.md) and add any that are missing.

3. Add the discovery source. In the Global scope, create a fix script from
   [`integrations/servicenow/global/register-discovery-source.js`](../integrations/servicenow/global/register-discovery-source.js),
   leave **Unloadable** cleared, and run it. It adds the `Nischoy Topo`
   discovery source choice if it is not already there.

Do not mix the two methods on one instance. If the app was installed from the
XML, upgrade it with the XML.

## Next step

With both parts installed, follow [your first ServiceNow discovery run](first-success.md).
