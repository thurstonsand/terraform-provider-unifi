package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// identityUpgradeResource is the pair of interfaces every resource with a
// versioned identity implements.
type identityUpgradeResource interface {
	fwresource.ResourceWithIdentity
	fwresource.ResourceWithUpgradeIdentity
}

// versionedIdentityResources lists every resource whose identity schema changed
// after v0.55.0 and therefore has to upgrade identity data stored by it.
func versionedIdentityResources() map[string]identityUpgradeResource {
	return map[string]identityUpgradeResource{
		"ap_group":         &apGroupResource{},
		"client_qos_rate":  &clientQosRateResource{},
		"device":           &deviceResource{},
		"dns_record":       &dnsRecordFrameworkResource{},
		"firewall_group":   &firewallGroupResource{},
		"firewall_policy":  &firewallPolicyResource{},
		"firewall_rule":    &firewallRuleResource{},
		"firewall_zone":    &firewallZoneResource{},
		"network":          &networkResource{},
		"port_forward":     &portForwardResource{},
		"port_profile":     &portProfileResource{},
		"power_supervisor": &powerSupervisorResource{},
		"radius_profile":   &radiusProfileResource{},
		"radius_user":      &radiusUserResource{},
		"site_to_site_vpn": &siteToSiteVPNResource{},
		"static_route":     &staticRouteFrameworkResource{},
		"traffic_route":    &trafficRouteResource{},
		"vpn_client":       &vpnClientResource{},
		"vpn_server":       &vpnServerResource{},
		"wan":              &wanResource{},
		"wireguard_peer":   &wireguardPeerResource{},
		"wlan":             &wlanFrameworkResource{},
	}
}

// TestUpgradeLegacyIdentity runs each resource's version 0 upgrader over both
// identity shapes that were written under that version: the bare id from
// v0.55.0, and the id plus site that v0.56.0-ansiblonomicon.1 wrote before the
// schema was versioned. The result has to fit the current identity schema, keep
// the id, and carry a stored site through.
func TestUpgradeLegacyIdentity(t *testing.T) {
	ctx := context.Background()
	const (
		legacyID   = "legacy-id"
		legacySite = "legacy-site"
	)

	payloads := []struct {
		name     string
		json     string
		wantSite string
	}{
		{name: "id only", json: `{"id":"` + legacyID + `"}`},
		{
			name:     "id and site",
			json:     `{"id":"` + legacyID + `","site":"` + legacySite + `"}`,
			wantSite: legacySite,
		},
	}

	for name, r := range versionedIdentityResources() {
		t.Run(name, func(t *testing.T) {
			var schemaResp fwresource.IdentitySchemaResponse
			r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, &schemaResp)

			if got := schemaResp.IdentitySchema.Version; got != identitySchemaVersion {
				t.Fatalf("identity schema version = %d, want %d", got, identitySchemaVersion)
			}

			upgraders := r.UpgradeIdentity(ctx)
			upgrader, ok := upgraders[0]
			if !ok {
				t.Fatalf("no identity upgrader for version 0, have %v", upgraders)
			}
			if upgrader.PriorSchema == nil {
				t.Fatal("upgrader has no prior schema")
			}

			for _, payload := range payloads {
				t.Run(payload.name, func(t *testing.T) {
					priorType := upgrader.PriorSchema.Type().TerraformType(ctx)
					priorRaw, err := (&tfprotov6.RawState{JSON: []byte(payload.json)}).
						Unmarshal(priorType)
					if err != nil {
						t.Fatalf("decoding legacy identity against prior schema: %v", err)
					}

					req := fwresource.UpgradeIdentityRequest{
						Identity: &tfsdk.ResourceIdentity{
							Raw:    priorRaw,
							Schema: *upgrader.PriorSchema,
						},
					}
					resp := fwresource.UpgradeIdentityResponse{
						Identity: &tfsdk.ResourceIdentity{
							Schema: schemaResp.IdentitySchema,
						},
					}

					upgrader.IdentityUpgrader(ctx, req, &resp)

					if resp.Diagnostics.HasError() {
						t.Fatalf("upgrading identity: %v", resp.Diagnostics)
					}
					currentType := schemaResp.IdentitySchema.Type().TerraformType(ctx)
					if !resp.Identity.Raw.Type().Equal(currentType) {
						t.Fatalf(
							"upgraded identity type = %s, want %s",
							resp.Identity.Raw.Type(),
							currentType,
						)
					}

					attrs := identityAttributes(t, resp.Identity.Raw)
					if _, hasID := schemaResp.IdentitySchema.Attributes["id"]; hasID {
						var got string
						if err := attrs["id"].As(&got); err != nil {
							t.Fatalf("reading upgraded id: %v", err)
						}
						if got != legacyID {
							t.Errorf("upgraded id = %q, want %q", got, legacyID)
						}
					}

					if site, hasSite := attrs["site"]; hasSite && payload.wantSite != "" {
						var got string
						if err := site.As(&got); err != nil {
							t.Fatalf("reading upgraded site: %v", err)
						}
						if got != payload.wantSite {
							t.Errorf("upgraded site = %q, want %q", got, payload.wantSite)
						}
					}

					for attr, value := range attrs {
						if attr == "id" {
							continue
						}
						if attr == "site" && payload.wantSite != "" {
							continue
						}
						if !value.IsNull() {
							t.Errorf(
								"upgraded identity attribute %q = %s, want null: "+
									"Read fills it from state, and it can only do that while the "+
									"attribute is null",
								attr,
								value,
							)
						}
					}
				})
			}
		})
	}
}

// TestUpgradeLegacyDeviceIdentityIsFullyNull covers the one resource whose
// identity key itself changed. The v0 identity held the device id and the
// current one holds the MAC, so nothing carries over. A fully null identity is
// the only kind Read is allowed to replace, which is what lets Read recover the
// MAC from state.
func TestUpgradeLegacyDeviceIdentityIsFullyNull(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}

	var schemaResp fwresource.IdentitySchemaResponse
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, &schemaResp)

	upgrader := r.UpgradeIdentity(ctx)[0]
	priorRaw, err := (&tfprotov6.RawState{JSON: []byte(`{"id":"device-id"}`)}).
		Unmarshal(upgrader.PriorSchema.Type().TerraformType(ctx))
	if err != nil {
		t.Fatalf("decoding legacy device identity: %v", err)
	}

	req := fwresource.UpgradeIdentityRequest{
		Identity: &tfsdk.ResourceIdentity{Raw: priorRaw, Schema: *upgrader.PriorSchema},
	}
	resp := fwresource.UpgradeIdentityResponse{
		Identity: &tfsdk.ResourceIdentity{Schema: schemaResp.IdentitySchema},
	}

	upgrader.IdentityUpgrader(ctx, req, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("upgrading device identity: %v", resp.Diagnostics)
	}
	if resp.Identity.Raw.IsNull() {
		t.Fatal("upgraded device identity is null; Terraform rejects an absent identity")
	}
	if !resp.Identity.Raw.IsFullyNull() {
		t.Errorf("upgraded device identity = %s, want every attribute null", resp.Identity.Raw)
	}
}

// TestReadPassesUpgradedIdentityThrough refreshes a firewall zone whose stored
// identity carries no site, the shape an upgraded v0.55.0 identity has. Read has
// to take site from state and hand the identity back untouched; anything else is
// an "Unexpected Identity Change" from the framework.
func TestReadPassesUpgradedIdentityThrough(t *testing.T) {
	ctx := context.Background()
	r := &firewallZoneResource{
		client: newFirewallZoneFakeControllerClient(t, map[string][]unifi.FirewallZone{
			"default": {{ID: "zone-id", Name: "DMZ", ZoneKey: "dmz"}},
		}),
	}

	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	var identityResp fwresource.IdentitySchemaResponse
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, &identityResp)

	state := tfsdk.State{
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
		Schema: schemaResp.Schema,
	}
	for attr, value := range map[string]string{
		"id":       "zone-id",
		"site":     "default",
		"name":     "DMZ",
		"zone_key": "dmz",
	} {
		if diags := state.SetAttribute(ctx, path.Root(attr), value); diags.HasError() {
			t.Fatalf("setting state attribute %q: %v", attr, diags)
		}
	}

	// The identity an upgraded v0.55.0 state carries: an id and nothing else.
	identity := &tfsdk.ResourceIdentity{
		Raw: tftypes.NewValue(
			identityResp.IdentitySchema.Type().TerraformType(ctx),
			map[string]tftypes.Value{
				"id":   tftypes.NewValue(tftypes.String, "zone-id"),
				"site": tftypes.NewValue(tftypes.String, nil),
			},
		),
		Schema: identityResp.IdentitySchema,
	}

	req := fwresource.ReadRequest{
		State: state,
		Identity: &tfsdk.ResourceIdentity{
			Raw:    identity.Raw.Copy(),
			Schema: identity.Schema,
		},
	}
	resp := &fwresource.ReadResponse{
		State: tfsdk.State{Raw: state.Raw.Copy(), Schema: state.Schema},
		Identity: &tfsdk.ResourceIdentity{
			Raw:    identity.Raw.Copy(),
			Schema: identity.Schema,
		},
	}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}
	if !resp.Identity.Raw.Equal(identity.Raw) {
		t.Errorf("Read returned identity %s, want %s unchanged", resp.Identity.Raw, identity.Raw)
	}

	var site string
	if diags := resp.State.GetAttribute(ctx, path.Root("site"), &site); diags.HasError() {
		t.Fatalf("reading refreshed site: %v", diags)
	}
	if site != "default" {
		t.Errorf("refreshed site = %q, want %q", site, "default")
	}
}

// identityAttributes unpacks an identity object value into its attributes.
func identityAttributes(t *testing.T, value tftypes.Value) map[string]tftypes.Value {
	t.Helper()

	attrs := map[string]tftypes.Value{}
	if err := value.As(&attrs); err != nil {
		t.Fatalf("unpacking identity %s: %v", value, err)
	}
	return attrs
}
