# Publishing Stave releases

The root Go module is the only published Stave package. The nested SSH, Bubble
Tea, and Lip Gloss modules remain internal until they receive independent
versions and tags.

## Publish the prepared release candidate

Read the candidate from `.release-please-manifest.json`; publish it from the
hardened final `main` commit, never from an older release-please pull request
merge commit.

1. Merge the hosted-runner migration and the Stave ARC retirement. Before
   changing repository visibility, verify Flux has pruned the Stave ARC
   HelmRelease and that its scale set, listener, and pods are absent. Confirm
   no repository or organization runner eligibility remains where applicable.
2. On the final `main` commit, wait for a fresh successful required-check run.
   Record its commit SHA and confirm it contains the intended candidate
   manifest, changelog, and release workflow.
3. Derive the release tag from the manifest and tag that exact SHA. Do not move
   or recreate the tag after it is pushed:

   ```sh
   set -eu
   git switch main
   git pull --ff-only
   test -z "$(git status --short)"
   final_main_sha="$(git rev-parse HEAD)"
   test "${final_main_sha}" = "${verified_main_sha:?set this to the successful required-check SHA}"
   candidate_version="$(node -p "require('./.release-please-manifest.json')['.']")"
   release_tag="v${candidate_version}"
   git tag -a "${release_tag}" "${final_main_sha}" -m "Release ${release_tag}"
   git push origin "${release_tag}"
   ```

4. The tag triggers [the release workflow](../.github/workflows/release.yml).
   It validates the tag, runs `make ci` and `make release-contract`, then
   publishes a prerelease with `CHANGELOG.md`, `LICENSE`, and the performance
   evidence artifact. Wait for that workflow and verify all published assets.
5. Change repository visibility only after the Stave ARC prune and eligibility
   proof in step 1 passes.
   Then run a controlled real external fork pull request and prove it uses the
   hosted untrusted path before approving general outside workflow runs.
6. From a clean module outside this repository, resolve the public module at
   the exact tag, for example:

   ```sh
   go list -m -json "github.com/ben-ranford/stave@${release_tag}"
   ```

7. After the tag, prerelease, assets, and public Go resolution are verified,
   replace `autorelease: pending` with `autorelease: tagged` on the original
   release-please pull request. `skip-github-release: true` delegates
   publication to the tag workflow, so release-please does not make that label
   transition itself.

Release Please updates the annotated candidate versions in `README.md` and
`docs/client-adoption.md` for future release pull requests. Keep the
annotations on their version lines.

## GA promotion gate

Do not publish a GA tag until `make release-ga-contract` passes from the final
release commit. That gate requires immutable, published Lopper proving-client
evidence for parity and rollback, not local-worktree claims. Lopper should
record `github.com/ben-ranford/stave` at the released version in its `go.mod`.
Homebrew, if used, ships a separately released Lopper binary only after Lopper
CI passes; it is not a Stave distribution channel. Record the immutable Lopper
revision and its passing parity and rollback evidence with the GA release
decision.
