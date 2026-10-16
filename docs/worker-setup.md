# Guided Linux worker setup

This command is new source work. It is **not included in the published 0.4.7
worker**, and Topo is not yet available in default Debian, Ubuntu or Fedora
repositories. Continue using the [current installation guide](distribution.md)
for that release. Default-repository publication and the complete three-step
onboarding flow remain pending.

## What the assistant does

For a packaged Linux host running systemd, the proposed entry point is one
interactive command:

```sh
sudo topo worker setup
```

It asks for your ServiceNow instance, existing worker pool and site, a protected
OAuth token **file reference**, an approved IPv4 allowlist file, and an
independently verified SSH `known_hosts` file. Enter file references rather than
token or password values. It configures the packaged SSH-only worker service,
checks registration as the service account, and asks before enabling and starting
the service. It does not change the input files or replace an existing deployment.

This is a Linux SSH setup path. It does not configure Windows discovery, create
ServiceNow identities or profiles, obtain OAuth tokens, verify a target's identity
for you, or run a discovery task. A successful registration check establishes
neither target access nor CMDB delivery.

## Prepare the approved connection

The ServiceNow application must already be installed with its required indexes
and discovery source. An administrator and credential custodian must complete
the [identity, OAuth and profile setup](pilot-quickstart.md#2-create-the-least-privilege-servicenow-identities).
Leave schedules inactive while connecting a new worker.

Have these files ready on the discovery server:

| Input | Required content |
| --- | --- |
| OAuth token reference | `file:/absolute/path` to a restricted worker token. The source file must be owner-only. Never put its value in a command. |
| Target allowlist | Absolute path to approved canonical IPv4 CIDRs, one per line, with at most 1,024 selected addresses. Start with one `/32` target. |
| Verified host keys | Absolute path to SSH host keys covering the selected target addresses on port 22. Confirm fingerprints through an independent trusted source; `ssh-keyscan` alone is insufficient. |

Files must be regular files owned by root or the invoking user; symlinks are
rejected. Policy files must not be writable by other users. The assistant copies
approved inputs into restricted service files under `/etc/topo-worker`.

## Connect and start

Run the command from an interactive terminal on a host with the matching Topo
package and its dormant `topo-worker.service`. Follow the prompts. Confirm that
host identities were independently verified, then approve writing the new
configuration. The assistant performs the same registration and heartbeat check
as the packaged service, using the `topo-worker` account.

For a new deployment, the registration check uses protected staged files before
the configuration is saved. After a ready response, approve starting the service. The assistant enables it
for subsequent boots and checks that it is active. It does not activate a
ServiceNow schedule.

In ServiceNow, open **Nischoy Topo → Discovery Profiles**, select the SSH profile
for the configured pool and site, then click **Run now**. Inspect **Runs**,
collection errors, **IRE Deliveries**, and the resulting CMDB computers,
interfaces and ownership relationships. Repeat and inspect reconciliation before
enabling a schedule.

## Retry and existing installations

A failed initial preflight leaves no new configuration or running service.
Correct the settings or protected input files, then rerun setup to enter them
again. An intact configuration created by this assistant can be checked again without
rewriting it. Changed policy, extra files, existing unmanaged deployments,
running services or modified service units require manual operator review;
setup refuses to replace or activate them.

On a retry of a previously saved setup, a failed preflight preserves its files.
Correct remote access or deliberately renew the installed token following your
secure credential procedure before retrying. Editing the original token source
does not automatically rotate the installed copy.

If service activation fails, it may have partially enabled or started the unit.
Inspect `systemctl status topo-worker.service` before retrying. Keep existing
deployment files and follow the [manual worker guide](pilot-quickstart.md#4-install-and-configure-the-worker)
for deliberate configuration changes and upgrades.

## Eventual package-manager command

The intended second onboarding step will install Topo through the supported
distribution's default repository and launch setup on the same shell line.
The exact package name, supported distribution releases and command must be
published only after repository acceptance and clean installation tests.
An RPM specification, source package, PPA, Copr build or Nischoy repository does
not establish default-repository availability.
