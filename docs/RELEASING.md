# Releasing

Releases are cut from `main` by pushing a version tag. The `Release` workflow then builds and publishes everything; nothing is uploaded by hand.

## Cut a release

1. Make sure `main` is green.
2. In `CHANGELOG.md`, rename `## [Unreleased]` content into a new `## [X.Y.Z] - YYYY-MM-DD` section (keep an empty `## [Unreleased]` on top). The section text becomes the release notes; the release fails without it. Check with `scripts/release-notes.sh vX.Y.Z`.
3. Commit that through a pull request, then tag the merge commit:
   ```sh
   git switch main && git pull
   git tag -s vX.Y.Z -m "vX.Y.Z"      # or -a if you do not sign
   git push origin vX.Y.Z
   ```
4. The workflow publishes:
   - the container image `ghcr.io/mrcodeeu/glucava` for `linux/amd64` and `linux/arm64`, tagged `X.Y.Z`, `X.Y` and `latest` (pre-releases like `v1.0.0-rc.1` get only their own tag), with provenance and SBOM attestations;
   - `glucava-linux-amd64`, `glucava-linux-arm64` (they need Chrome/Chromium installed) and `SHA256SUMS` on the GitHub release, with the CHANGELOG section as notes.

## Versions

`main` builds report `dev`. A release build reports its tag in the page footer, in `/health` (`build`) and in `glucava --version`. Local builds use `git describe` (`make build`).

## Verify an image

```sh
gh attestation verify oci://ghcr.io/mrcodeeu/glucava:X.Y.Z --owner MrCodeEU
```
