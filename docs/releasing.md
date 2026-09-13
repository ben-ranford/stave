# Publishing Stave releases

The root Go module is the only published Stave package. The nested SSH, Bubble
Tea, and Lip Gloss modules remain internal until they receive independent
versions and tags.

## Publish the prepared release candidate

Read the candidate from `.release-please-manifest.json`; publish it from the
hardened final `main` commit, never from an older release-please pull request
merge commit.

1. Merge the hosted-runner migration and the Stave-only ARC retirement. Before
   changing repository visibility, prove GitHub has deregistered Stave's
   scheduler boundary: the current GitOps deletion is applied, the repository's
   persistent runner scale-set record is gone, and the repository's
   self-hosted-runner inventory is empty. Repeat those two GitHub observations
   after the reconciliation interval; an idle runner count of zero alone is
   not evidence of deregistration. The scale-set page is
   [`Settings > Actions > Runner scale sets`](https://github.com/ben-ranford/stave/settings/actions/runner-scale-sets/1).
   If cluster access is available, separately record Flux, HelmRelease,
   listener, and pod cleanup. Do not represent that physical cleanup as
   verified when only the GitHub scheduler-boundary proof is available.
2. On the final `main` commit, wait for a fresh successful required-check run.
   Record its commit SHA and confirm it contains the intended candidate
   manifest, changelog, and release workflow.
3. Obtain the authorized visibility transition and make the repository public
   only after steps 1 and 2 have both passed. Do not tag a private repository:
   the tag workflow's anonymous public-consumer probe cannot access a private
   tag through the public GitHub API, Go proxy, or checksum database. Record
   the visibility transition with the scheduler-boundary and source-CI proof.
4. Run a controlled real public-fork pull request and prove it uses the hosted
   untrusted path before approving general outside workflow runs.
5. Derive the release tag from the manifest and tag that exact SHA. Do not move
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

6. The tag triggers [the release workflow](../.github/workflows/release.yml).
   It validates the tag, runs `make ci` and `make release-contract`, then
   publishes a prerelease with `CHANGELOG.md`, `LICENSE`, and the performance
   evidence artifact. Its final `release / public consumer` job waits for public
   propagation, then resolves the exact module through `proxy.golang.org` and
   `sum.golang.org`, runs the quick-start consumer, and checks the three release
   asset SHA-256 digests. The job reports the annotated tag object SHA, peeled
   source commit SHA, canonical module version, module sum, and asset digests.
   Build metadata remains part of the requested release tag. Go can select a
   canonical version without that metadata or a pseudo-version, depending on
   the repository and tag state; the probe compares the selected module version
   with the requested-tag resolution and retains the literal tag and source
   provenance checks. If a module download omits provenance, the probe reads
   the selected canonical version's provenance metadata from the public proxy
   in a fresh module cache; missing or mismatched provenance fails the check.
   The downloaded module remains verified through the checksum database. It
   makes only anonymous public reads and never changes tags, releases, or labels.
7. From a clean module outside this repository, resolve the public module at
   the exact tag, for example:

   ```sh
   go list -m -json "github.com/ben-ranford/stave@${release_tag}"
   ```

8. After the tag, prerelease, assets, and `release / public consumer` job are
   verified, replace `autorelease: pending` with `autorelease: tagged` on the original
   release-please pull request. `skip-github-release: true` delegates
   publication to the tag workflow, so release-please does not make that label
   transition itself.

   If public propagation is delayed, rerun only the failed workflow job after
   confirming the immutable tag still points at the recorded source SHA. The
   probe retries each public fetch or incomplete release metadata three times at
   five-second intervals. Each HTTP request is limited to 15 seconds and each
   public Go resolution attempt to 60 seconds, with a two-second TERM-to-KILL
   cleanup window. These settings bound publication retries, but `go list` and
   `go run` intentionally remain outside that helper so they can validate the
   resolved module and example; the workflow job timeout remains the outer cap,
   not a claimed total script deadline. A failed
   job is not evidence to set the release label. For an operator-only retry,
   run `RELEASE_PROBE_ATTEMPTS=3 RELEASE_PROBE_RETRY_SECONDS=5
   ./scripts/rigor/verify-published-release.sh "$release_tag"` from any clean
   checkout. Do not add credentials, `go.work`, private proxy settings, or a
   local `replace` directive.

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

For a v1 GA tag, release verification also runs `make release-baseline`. It selects the newest
annotated stable v1 tag, regenerates both public API inventories from source,
and records the tag commit and Go floors before allowing a minor release. If
only release candidates exist, this is an intentional blocker. The opt-in
`make release-baseline-development` comparison against `v1.0.0-rc.2` is not a
substitute for that stable baseline.
