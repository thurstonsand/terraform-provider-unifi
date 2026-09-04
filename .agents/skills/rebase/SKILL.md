---
name: rebase
description: Rebase and release Thurston's permanent UniFi provider and go-unifi forks when upstream changes or ansiblonomicon needs a new provider version.
---

# Rebase

Maintain the `release` branches in the sibling `terraform-provider-unifi` and `go-unifi` forks. Neither fork targets an upstream pull request. The release branch is the product branch, and it is collapsed to one coherent commit over `upstream/main` at each rebase.

## Shape of the fork

Between rebases each fork accumulates commits: a feature, then the hotfixes that follow it into ansiblonomicon. That is expected. Each provider commit that reaches ansiblonomicon ships under its own tag; the SDK is consumed by commit, so its history is just history. The squash to a single commit happens as part of the next rebase onto upstream, not while hotfixes are still landing.

Take stock before touching anything:

```sh
for repo in ../go-unifi .; do
  git -C "$repo" rev-list --count upstream/main..release
  git -C "$repo" log --oneline upstream/main..release
 done
```

Read the list and understand what each commit is for. Stop if a commit is not one of yours, and do not reconstruct an unexpected history.

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

What the fork actually carries, and therefore what to check:

- SDK: the mDNS setting's `enabled_for` / `enabled_for_network_ids` scope, and service lists that stay serialized when empty so clearing them is not a silent no-op
- SDK: `mac_override` / `mac_override_enabled` in the WAN branch of `Network.MarshalJSON`, sent without `omitempty` so an empty value clears the clone
- SDK: generator injections in `cmd/fields/compat.go` for fields the 10.x JAR spec dropped but controllers still honor — usg GeoIP filtering, nested IPS suppression, WLAN `bandsteering_mode`, device radio assisted roaming, port-override tagged networks
- provider: the `mdns` block on `unifi_setting`, read-modify-write over the remote setting
- provider: `mac_override` on `unifi_wan`, sensitive, validated without echoing, and clearable by deleting it from HCL
- provider: `identitySchemaVersion` and the version 0 identity upgraders in `unifi/identity_upgrade.go`

If upstream now supplies equivalent behavior, remove that part from the fork. Prefer upstream's schema unless it cannot express ansiblonomicon's declaration.

Identity schema versions need their own reconciliation. The version number is state that ansiblonomicon already wrote, so it cannot simply be renumbered. If upstream lands its own identity upgrade and bumps the version, take upstream's number, drop the fork's upgraders in favor of theirs where the shapes match, and confirm against a `tofu plan` that state written under the fork's version still converts. If upstream's number is lower than the fork's, keep the fork's, and keep the version 0 upgraders that accept both `{id}` and `{id,site}` payloads: released state carries both shapes under version 0.

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

Update its `go.mod` replacement to the pseudo-version of the SDK release commit when the SDK fork is still needed (see below); the SDK fork is not tagged. Preserve upstream's provider patterns, state upgraders, generated documentation, and release workflow.

Run:

```sh
gofmt -w unifi
go test ./...
go build ./...
```

Review both complete diffs against upstream. Every remaining line must implement a capability ansiblonomicon still uses.

## Versioning

Only the provider is tagged. Follow the newest upstream release version with a prerelease suffix that counts fork releases:

```text
upstream v0.56.0 -> v0.56.0-ansiblonomicon.1, then .2, .3 …
```

Every hotfix that reaches ansiblonomicon gets its own suffix bump. Reset the counter to `.1` when upstream releases a new version. Never reuse a published tag. Record the exact upstream base commit in each release note.

The SDK carries no fork tag. The provider consumes it as a Go pseudo-version off the pushed release commit, which is why the SDK has to be pushed first:

```text
replace github.com/ubiquiti-community/go-unifi => github.com/thurstonsand/go-unifi v1.34.1-0.20260904021401-9aa5323cb361
```

## Publish SDK first

Push the SDK release branch, then pin the provider to that commit:

```sh
git -C ../go-unifi push --force-with-lease origin release
(cd .. && GOFLAGS=-mod=mod go get github.com/thurstonsand/go-unifi@<sha>)
```

`go get` writes the pseudo-version, which encodes the base version, the commit time, and the short sha. Confirm `go.mod` and `go.sum` picked it up and that `go build ./...` resolves it from the module proxy rather than a local cache. A pseudo-version only resolves once the commit is public, so this ordering is not optional.

Draft the provider's annotated tag message and present it to Thurston before creating the tag.

## Publish provider

The provider tag triggers the GitHub Actions GoReleaser workflow. Require release ZIPs for Darwin and Linux on AMD64 and ARM64 plus `SHA256SUMS`.

After the workflow completes:

1. Download each target archive.
2. Verify it against `SHA256SUMS`.
3. Run the Darwin ARM64 binary through an ansiblonomicon development plan.
4. Prepare the ansiblonomicon release manifest with the individual archive hashes.
5. Regenerate `.terraform.lock.hcl` for every supported platform.
6. Present the provider and ansiblonomicon diffs before changing shared R2 state.

Push release branches with `--force-with-lease`; push approved tags without force.

## State migration

A provider source change is global because ansiblonomicon uses shared R2 state. Before the one-time `tofu state replace-provider`, stream `tofu state pull` directly into the established encrypted network-backup format. Never write plaintext state to disk. The migration command and state snapshot remain outside Git.

## Keep this skill accurate

After each rebase or release, update this skill when the actual process differed. The next rebase should inherit evidence, not folklore.
