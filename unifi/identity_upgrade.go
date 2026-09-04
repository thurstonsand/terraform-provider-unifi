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

// legacyIDIdentityModel is the version 0 identity of nearly every resource: the
// object id, and a site that may or may not be there. v0.55.0 wrote the id
// alone; v0.56.0-ansiblonomicon.1 added site while the schema was still
// unversioned, so both shapes exist in the wild under version 0 and the prior
// schema has to decode either one.
type legacyIDIdentityModel struct {
	ID   types.String `tfsdk:"id"`
	Site types.String `tfsdk:"site"`
}

// legacyIDIdentitySchema describes [legacyIDIdentityModel] so the framework can
// hand the upgrader a decoded identity instead of raw JSON.
func legacyIDIdentitySchema() *identityschema.Schema {
	return &identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
			"site": identityschema.StringAttribute{
				OptionalForImport: true,
			},
		},
	}
}

// upgradeLegacyIDIdentity builds the version 0 to 1 identity upgrade for
// resources whose version 0 identity was keyed on the object id. newIdentity
// maps that id, and the site if the stored identity carried one, onto the
// resource's current identity model.
//
// Attributes the stored identity never held stay null. Read then takes its
// lookup values from ordinary state, which still carries site and mac, and must
// return the upgraded identity unchanged: the framework rejects any Read that
// alters an identity unless every attribute of it is null.
func upgradeLegacyIDIdentity(
	newIdentity func(id, site types.String) any,
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

				resp.Diagnostics.Append(resp.Identity.Set(ctx, newIdentity(prior.ID, prior.Site))...)
			},
		},
	}
}
