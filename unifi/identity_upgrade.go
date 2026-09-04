package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// identitySchemaVersion is the identity schema version of every resource whose
// identity changed shape after v0.55.0: most gained an optional site, and
// unifi_device replaced id with mac.
//
// The version matters because Terraform converts the identity stored in state
// to the current identity schema before it ever reaches Read. An identity
// object written by v0.55.0 has no site attribute at all, and that conversion
// fails with `attribute "site" is required`, which no Read implementation can
// intercept. A version bump routes those stored identities through
// UpgradeIdentity first.
const identitySchemaVersion = 1

// legacyIDIdentityModel is the v0.55.0 identity of nearly every resource: the
// bare object id.
type legacyIDIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

// legacyIDIdentitySchema describes [legacyIDIdentityModel] so the framework can
// hand the upgrader a decoded identity instead of raw JSON.
func legacyIDIdentitySchema() *identityschema.Schema {
	return &identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
		},
	}
}

// upgradeLegacyIDIdentity builds the version 0 to 1 identity upgrade for
// resources whose v0.55.0 identity held only the object id. newIdentity maps
// that id onto the resource's current identity model.
//
// Attributes the v0 identity never stored stay null. Read then takes its lookup
// values from ordinary state, which still carries site and mac, and must return
// the upgraded identity unchanged: the framework rejects any Read that alters
// an identity unless every attribute of it is null.
func upgradeLegacyIDIdentity(
	newIdentity func(id types.String) any,
) map[int64]resource.IdentityUpgrader {
	return map[int64]resource.IdentityUpgrader{
		0: {
			PriorSchema: legacyIDIdentitySchema(),
			IdentityUpgrader: func(
				ctx context.Context,
				req resource.UpgradeIdentityRequest,
				resp *resource.UpgradeIdentityResponse,
			) {
				var prior legacyIDIdentityModel
				resp.Diagnostics.Append(req.Identity.Get(ctx, &prior)...)
				if resp.Diagnostics.HasError() {
					return
				}

				resp.Diagnostics.Append(resp.Identity.Set(ctx, newIdentity(prior.ID))...)
			},
		},
	}
}
