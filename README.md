# Unifi Terraform Provider (terraform-provider-unifi)

This is Thurston Sandberg's permanent fork for controller fields used by [ansiblonomicon](https://github.com/thurstonsand/ansiblonomicon) that upstream does not expose. The `release` branch stays rebased directly on `ubiquiti-community/main`; `.agents/skills/rebase/SKILL.md` defines the maintenance and release procedure. GitHub Releases publish the platform binaries, and ansiblonomicon verifies and installs them through OpenTofu's implied filesystem mirror. This fork is not published to an OpenTofu or Terraform registry.

[![Acceptance Tests](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml/badge.svg)](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml) [![codecov](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi/graph/badge.svg?token=KVP7FS41IG)](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi)

> **Note**: You can't (for obvious reasons) configure your network while connected to something that may disconnect (like the WiFi). Use a hard-wired connection to your controller to use this provider.

Functionality first needs to be added to the [go-unifi](https://github.com/ubiquiti-community/go-unifi) SDK.

## Documentation

You can browse documentation on the [Terraform provider registry](https://registry.terraform.io/providers/ubiquiti-community/unifi/latest/docs).

## Supported Unifi Controller Versions

As of version [v0.34](https://github.com/ubiquiti-community/terraform-provider-unifi/releases/tag/v0.34.0), this provider only supports version 6 of the Unifi controller software. If you need v5 support, you can pin an older version of the provider.

The docker, UDM, and UDM-Pro versions are slightly different (the API is proxied a little differently) but for the most part should all be supported. Individual patch versions of the controller are generally not tested for compatibility, just the latest stable versions.

## Using the Provider

### Terraform 1.0 and above

You can use the provider via the [Terraform provider registry](https://registry.terraform.io/providers/ubiquiti-community/unifi).
