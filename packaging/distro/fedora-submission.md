# Fedora review and maintainer checklist

This is submission preparation, not an accepted Fedora package. No Fedora
account, agreement, review ticket, Copr project or dist-git repository has been
created by this work. The public maintainer contact is Soumik Sarkar
<soumiks@gmail.com>.

## Owner account steps

1. Create or use your account at https://accounts.fedoraproject.org/ with
   `soumiks@gmail.com`. In Profile → Agreements, personally read and accept
   the Fedora Project Contributor Agreement.
2. Create or use a separate account at https://bugzilla.redhat.com/ with the
   same email address. Fedora links packaging privileges through that address.
3. Sign in at https://copr.fedorainfracloud.org/ with your Fedora account.
   Share your public Fedora username and whether you already belong to the
   `packager` group. Passwords, private keys and API tokens are not needed
   in chat. Add your public SSH key to the Fedora account when setting up
   authenticated package maintenance; retain the private key locally.
4. Plan to maintain the package: respond to review comments, update bundled
   dependencies, rebuild for Fedora changes and handle security reports.
   A reviewer or sponsor cannot take this responsibility implicitly.

## Review hosting and submission

First check https://packages.fedoraproject.org/,
https://fedoraproject.org/PackageReviewStatus/ and the orphan/retired-package
dashboard for an existing package or review. Repeat this immediately before
filing; a search-engine miss is not name clearance.

Upload the completed source RPM to a Copr project and build at least Rawhide.
Copy the direct public spec and source-RPM URLs from the resulting build.
The GitHub Actions artifacts are temporary authenticated CI downloads, not
the permanent anonymous URLs reviewers need. Copr is review hosting and a
builder; a Copr repository is not Fedora default-repository admission.

Use the [Fedora review form](https://bugzilla.redhat.com/bugzilla/enter_bug.cgi?product=Fedora&format=fedora-review).
The prepared text below deliberately leaves account/hosting fields unfilled
until those resources exist. Do not submit with placeholders or failed tests.

## Prepared review text

```text
Summary: Review Request: topo - Destination-neutral infrastructure discovery worker and CLI

Spec URL: [direct public URL from successful Copr review build]
SRPM URL: [direct public URL from the same source snapshot]
Fedora Account System Username: [owner's actual Fedora username]
Maintainer: Soumik Sarkar <soumiks@gmail.com>
Upstream: https://github.com/Nischoy-ai/topo
Source snapshot: [full reviewed commit from source-manifest.json]
License: Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC-BY-2.5 AND CC0-1.0 AND ISC AND LicenseRef-Fedora-Public-Domain AND Linux-man-pages-copyleft AND MIT AND SunPro AND bcrypt-Solar-Designer

Topo discovers infrastructure through locally approved operations and emits
destination-neutral observations. Its optional outbound worker supports
ServiceNow-managed Linux SSH and restricted Windows HTTPS computer/interface
inventory. Credentials, verified trust files and separate administrator
preparation are required before discovery. Installing this package does not
start discovery. It builds independently from public source.

This review uses an unreleased source snapshot because the worker setup
assistant is not part of the published 0.4.7 worker. Existing upstream tags,
released assets and Nischoy repository packages remain unchanged.

The vendor archive is generated with Fedora Go Vendor Tools from unchanged
upstream go.mod/go.sum. All required composite-license, musl-header and artwork
notices are retained. No license files or directories are excluded.
The spec uses Fedora compiler macros, sysusers and standard systemd lifecycle
macros. Both packaged services are disabled until explicitly configured.

Build/test evidence: [attach final source-manifest, mock, license, rpmlint,
native compiler/vet/race/vulnerability and systemd lifecycle results].
Copr Rawhide build: [successful build URL].
Architecture scope: [actual successful x86_64/aarch64 targets].
Known limitations: synthetic loopback service tests do not establish real
ServiceNow/IRE, target compatibility, reboot, production readiness or default
repository publication.
```

If this is your first package and you are not a sponsored packager, add:

```text
This is my first Fedora package submission and I need a packager sponsor.
I maintain the upstream Topo project and will maintain its Fedora packaging.
I would welcome review and mentorship from a Fedora Go SIG packager.
```

Use that maintenance statement only if you accept that responsibility.
Set `FE-NEEDSPONSOR` in the review bug's Blocks field when applicable.
Do not claim current packager-group membership before it is confirmed.

## Sponsorship and acceptance

A package reviewer approves the package; a sponsor separately mentors and
admits you to Fedora's packager group. Sponsorship is not a payment. Participate
in reviews and demonstrate that you understand Fedora packaging guidelines.
If the package is approved but you still need sponsorship, request it at
https://forge.fedoraproject.org/packaging/sponsors/issues and link the review.

After review approval and sponsorship, request the dist-git repository with
`fedpkg request-repo topo REVIEW_BUG_NUMBER`, import the reviewed sources and
build Rawhide in Koji. Additional supported Fedora branches require their own
branch/build/update process. Default-repository installation instructions can
be advertised only after Fedora publishes those builds; acceptance has no
promised date. A maintainer signing key is not required to prepare this unsigned
review SRPM.

## Official procedure checked

The October 16, 2026 preparation checked Fedora's official maintainer-docs Git
source at `003c0246bcea28c38a5a235e2ebec06870803ff6`:

- [Joining package maintainers](https://forge.fedoraproject.org/packaging/maintainer-docs/src/branch/main/modules/ROOT/pages/Joining_the_Package_Maintainers.adoc)
- [New contributor package process](https://forge.fedoraproject.org/packaging/maintainer-docs/src/branch/main/modules/ROOT/pages/New_Package_Process_for_New_Contributors.adoc)
- [Package review process](https://forge.fedoraproject.org/packaging/maintainer-docs/src/branch/main/modules/ROOT/pages/Package_Review_Process.adoc)
- [Packager sponsorship](https://forge.fedoraproject.org/packaging/maintainer-docs/src/branch/main/modules/ROOT/pages/How_to_Get_Sponsored_into_the_Packager_Group.adoc)
