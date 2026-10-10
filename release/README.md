# ServiceNow checksum manifests

These manifests pin the exact files for manual ServiceNow package delivery.
They authenticate package bytes when signed; they do not establish native
export provenance, installation acceptance or Store certification.

`servicenow-0.4.6-beta.SHA256SUMS` retains the original published package.
`servicenow-0.4.6-beta.2.SHA256SUMS` pins the revised installation package:
474 updates, the renamed Database Indexes child, and the bounded discovery-source
setup script. Application version remains 0.4.6; the suffix identifies package
revision 2. The XML's SHA-256 is
`d85b21f883bf5f2b599eaf570547c4d02a682f59dc5a1eaff8b4a6fa4d1374e9`.
Revision 2 is manually published with a protected checksum-manifest signature.
Its release notes record the exact signing source commit and verification command.

## Manual signing and publication

1. Inspect the exact native export and prepare the six package files plus
   `SHA256SUMS`. Preserve previously released bytes and their evidence.
2. Before requesting signing, validate all actual package files:

   ```sh
   python3 scripts/verify-servicenow-signing-input.py \
     dist/servicenow-0.4.6-beta.2 servicenow-0.4.6-beta.2
   ```

3. Run **Sign existing ServiceNow XML checksums** from the reviewed `main`
   commit, selecting the matching release. The existing protected environment
   requires independent review. Revision 2 signs the reviewed repository
   manifest before publication, using read-only repository permissions. This
   step validates manifest format and bytes, not package payloads. The original
   release path continues to download and check its already-published files.
4. Download the matching signature artifact from that exact successful run
   into a separate directory. Compare its `SHA256SUMS` with the reviewed
   package manifest and independently verify the bundle with Cosign:

   ```sh
   cosign verify-blob \
     --bundle <signature-directory>/SHA256SUMS.sigstore.json \
     --certificate-identity https://github.com/Nischoy-ai/topo/.github/workflows/sign-servicenow-xml.yml@refs/heads/main \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     --certificate-github-workflow-sha <exact-signing-run-source-commit> \
     dist/servicenow-0.4.6-beta.2/SHA256SUMS
   ```

5. Repeat the full payload verification before uploading. Manually attach the
   seven checked package files and signature bundle to the corresponding
   release. Record the signing source commit and verification command in its
   notes. Verify downloads before switching customer links. Keep the preceding
   release recoverable and preserve its tag, files and signatures.

The workflow has no release-writing permission and does not build, import,
export or publish customer packages. A signature authenticates the checksum
manifest; it is not a build or XML-export provenance attestation.
