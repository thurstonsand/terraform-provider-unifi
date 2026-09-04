---
name: rebase
description: Rebase and release Thurston's permanent UniFi provider and go-unifi forks when upstream changes or ansiblonomicon needs a new provider version.
---

# Rebase

Maintain the `release` branches in the sibling `terraform-provider-unifi` and `go-unifi` forks. Neither fork targets an upstream pull request. Keep each fork as one coherent commit over `upstream/main`; the release branch is the product branch.

## Invariant

For both repositories, require exactly one fork commit:

```sh
for repo in ../go-unifi .; do
  git -C "$repo" rev-list --count upstream/main..release
  git -C "$repo" log --oneline upstream/main..release
 done
```

Stop if either count differs from one. Do not reconstruct an unexpected history.

## Fetch without deleting fork tags

```sh
for repo in ../go-unifi .; do
  git -C "$repo" -c fetch.pruneTags=false fetch upstream --prune --no-tags
  git -C "$repo" -c fetch.pruneTags=false fetch upstream --tags
 done
```

Combining tag fetch with pruning can delete fork-only release tags because upstream does not have them.

## Delete upstreamed fork code first

Compare each fork commit with its new upstream. The goal is the required behavior with the smallest permanent fork, not preservation of our implementation.

Check at least:

- global mDNS mode, participating networks, predefined services, and custom services
- WAN MAC override value
- device Ethernet-to-network-group overrides
- PDU no-op normalization and relay-write exclusion
- any compatibility repairs carried because provider main moved ahead of go-unifi

If upstream now supplies equivalent behavior, remove that part from the fork. Prefer upstream's schema unless it cannot express ansiblonomicon's declaration.

## Rebase SDK, then provider

The provider consumes the SDK, so rebase `go-unifi` first:

```sh
git -C ../go-unifi switch release
git -C ../go-unifi rebase upstream/main
```

Resolve conflicts in favor of upstream's generated-model conventions while retaining only missing fields and generator-preserving overrides. Squash back to one fork commit when the rebase creates more than one unique commit.

Run:

```sh
(cd ../go-unifi && gofmt -w cmd unifi && go test ./...)
```

Then rebase the provider:

```sh
git switch release
git rebase upstream/main
```

Update its `go.mod` replacement to the released `thurstonsand/go-unifi` version when the SDK fork is still needed. Preserve upstream's provider patterns, state upgraders, generated documentation, and release workflow.

Run:

```sh
gofmt -w unifi
go test ./...
go build ./...
```

Review both complete diffs against upstream. Every remaining line must implement a capability ansiblonomicon still uses.

## Versioning

Follow the newest upstream release version with a fork suffix:

```text
upstream v0.56.0 -> provider fork v0.56.0-1
upstream v1.35.0 -> SDK fork v1.35.0-1
```

If upstream has not released again and that fork version exists, increment only the suffix. Never reuse a published tag. Record the exact upstream base commit in each release note.

## Publish SDK first

Draft both annotated tag messages and present them to Thurston before creating tags. After approval, tag and push the SDK first so the provider's versioned replacement resolves from a public commit.

The SDK needs only its source tag. Verify it through a clean module download before publishing the provider.

## Publish provider

The provider tag triggers the GitHub Actions GoReleaser workflow. Require release ZIPs for Darwin and Linux on AMD64 and ARM64 plus `SHA256SUMS`.

After the workflow completes:

1. Download each target archive.
2. Verify it against `SHA256SUMS`.
3. Run the Darwin ARM64 binary through an ansiblonomicon development plan.
4. Prepare the ansiblonomicon release manifest with the individual archive hashes.
5. Regenerate `.terraform.lock.hcl` for every supported platform.
6. Present the provider and ansiblonomicon diffs before changing shared R2 state.

Push the rebased release branches with `--force-with-lease`; push approved tags without force.

## State migration

A provider source change is global because ansiblonomicon uses shared R2 state. Before the one-time `tofu state replace-provider`, stream `tofu state pull` directly into the established encrypted network-backup format. Never write plaintext state to disk. The migration command and state snapshot remain outside Git.

## Keep this skill accurate

After each rebase or release, update this skill when the actual process differed. The next rebase should inherit evidence, not folklore.
