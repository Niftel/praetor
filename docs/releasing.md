# Platform Release Promotion

Praetor releases are compatible component sets, not merely tags on this
repository. `platform-compatibility.yaml` is the release declaration and the
release preflight is the gate that verifies it.

## Promote a version

1. Choose the semantic version of each changed component. Components that have
   not changed retain their existing version.
2. Tag each changed component repository at `v<component version>` and publish
   its service image at the same component version. A
   commit-addressed image may be used during qualification, but the final
   manifest uses semantic tags.
3. Publish every component and shared Go module version declared by the
   manifest.
4. Update each changed `components.<name>.version` and the matching Helm
   `imageTags.<name>`. `platformVersion` advances only when publishing a new
   compatible platform set.
5. Update contract versions and database migration bounds when required.
6. Set `releaseStatus: stable` only after the candidate has passed platform and
   resilience testing.
7. Run:

   ```bash
   make compat-check
   make release-preflight-remote
   ```

8. Tag this repository with `v<platformVersion>` only after remote preflight
   succeeds.

The tag workflow rejects a Git tag that differs from the stable manifest
version. Remote verification deliberately happens before tagging: image build
workflows may also react to a tag, so attempting artifact verification on the
same event would introduce a race with publication. The release-preflight
workflow can also be dispatched manually to repeat remote verification.

## Development manifests

While `releaseStatus` is `development`, ordinary `make compat-check` succeeds so
the component set can be developed and tested. `make release-preflight` fails by
design. This prevents an incomplete candidate from being promoted merely because
its YAML is structurally valid.

## What the gate proves

The local gate verifies:

- Stable release status
- Matching component versions and Helm per-component image tags
- Complete first-party component inventory
- Contract versions aligned with this repository's `go.mod`
- A database migration range ending at the latest numbered migration

The remote gate additionally verifies that every component repository tag,
GHCR image manifest, component Go module, and shared contract module can be
resolved. It does not push images, create tags, or mutate any repository.
