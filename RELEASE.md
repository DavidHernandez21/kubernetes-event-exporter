# Releasing

To publish a new container image release to GitHub Container Registry (GHCR):

## Trigger and validation rules

- The release workflow runs only on pushed Git tags.
- It does not run on branch pushes, pull requests, manual dispatches, or nightly schedules.
- A runtime validation step enforces strict semver tags in the exact format `vMAJOR.MINOR.PATCH`.
- Accepted example: `v1.2.3`
- Rejected examples: `v1.2`, `1.2.3`, `v1.2.3-rc1`, `v1.2.3+build.1`
- If validation fails, the workflow exits before any image is published.

1. Ensure all tests pass and your main branch is up to date.
2. Update the changelog and documentation as needed.
3. Create a new annotated semantic version tag (e.g., v1.2.3):
   ```sh
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```
   Only tags matching the pattern `vMAJOR.MINOR.PATCH` (e.g., v2.0.1) will trigger the release workflow.
4. The GitHub Actions workflow will automatically:
   - Build multi-architecture Docker images (amd64, arm64)
   - Push images to GHCR under `ghcr.io/<owner>/<repo>` (e.g., `ghcr.io/davidhernandez21/kubernetes-event-exporter`)
   - Tag images with both the release tag (e.g., v1.2.3) and the commit SHA (e.g., sha-abcdef1)
   - Attach provenance and SBOM for supply chain security

No manual publishing steps are required beyond pushing a valid semver tag.

See `.github/workflows/release.yml` for workflow details.
