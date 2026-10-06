package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

const pduTestMAC = "00:11:22:33:44:55"

// sanitizedPDUDevice models the Power Distribution Pro (model USPPDUP) the way
// the controller reports it: adopted, connected, outlet control on, and outlets
// whose relays carry live equipment.
func sanitizedPDUDevice() *unifi.Device {
	idx1 := int64(1)
	idx2 := int64(2)
	brightness := int64(80)
	return &unifi.Device{
		ID:      "000000000000000000000001",
		MAC:     pduTestMAC,
		Name:    "USP PDU Pro",
		Model:   "USPPDUP",
		Type:    "usw",
		Adopted: true,
		State:   unifi.DeviceStateConnected,
		ConfigNetwork: &unifi.DeviceConfigNetwork{
			Type: "dhcp",
			IP:   "10.10.10.215",
		},
		LcmBrightness:      &brightness,
		LcmNightModeBegins: "22:00",
		LcmNightModeEnds:   "08:00",
		OutletEnabled:      true,
		OutletOverrides: []unifi.DeviceOutletOverrides{
			{Index: &idx1, Name: "Outlet 1", RelayState: true, CycleEnabled: false},
			{Index: &idx2, Name: "Outlet 2", RelayState: true, CycleEnabled: false},
		},
	}
}

func deviceSchemaForTest(t *testing.T) tfsdk.State {
	t.Helper()
	r := &deviceResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	return tfsdk.State{Schema: resp.Schema}
}

func nullTimeouts() timeouts.Value {
	return timeouts.Value{
		Object: types.ObjectNull(map[string]attr.Type{
			"create": types.StringType,
			"read":   types.StringType,
			"update": types.StringType,
			"delete": types.StringType,
		}),
	}
}

// refreshedPDUModel builds the model an import followed by a refresh leaves in
// state: every controller-owned field from setResourceData, the plan-only flags
// normalized to their defaults, and the nested block collections resolved the
// way Read resolves them.
func refreshedPDUModel(t *testing.T) deviceResourceModel {
	t.Helper()
	ctx := context.Background()

	r := &deviceResource{}
	var diags diag.Diagnostics
	model := deviceResourceModel{Timeouts: nullTimeouts()}
	device := sanitizedPDUDevice()
	r.setResourceData(ctx, &diags, device, &model, "default")
	if diags.HasError() {
		t.Fatalf("setResourceData diagnostics: %v", diags)
	}

	model.AllowAdoption = types.BoolValue(true)
	model.ForgetOnDestroy = types.BoolValue(true)

	// Import leaves both block collections null; Read resolves them.
	portOverride, portDiags := r.refreshPortOverrideState(
		ctx, types.SetNull(types.ObjectType{AttrTypes: portOverrideAttrTypes()}),
		device.PortOverrides)
	if portDiags.HasError() {
		t.Fatalf("refreshPortOverrideState diagnostics: %v", portDiags)
	}
	ethernetOverride, ethDiags := refreshEthernetOverrideState(
		ctx, types.ListNull(ethernetOverrideObjectType()), device.EthernetOverrides)
	if ethDiags.HasError() {
		t.Fatalf("refreshEthernetOverrideState diagnostics: %v", ethDiags)
	}
	model.PortOverride = portOverride
	model.EthernetOverride = ethernetOverride

	return model
}

func stateFromModel(t *testing.T, model deviceResourceModel) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	state := deviceSchemaForTest(t)
	state.Raw = tftypes.NewValue(state.Schema.Type().TerraformType(ctx), nil)
	if d := state.Set(ctx, model); d.HasError() {
		t.Fatalf("state.Set diagnostics: %v", d)
	}
	return state
}

// copyObjectAttributes decodes an object value into a fresh map. tftypes hands
// back the value's own attribute map, so mutating the result of As would edit
// the value it came from.
func copyObjectAttributes(t *testing.T, v tftypes.Value) map[string]tftypes.Value {
	t.Helper()
	attrs := map[string]tftypes.Value{}
	if err := v.As(&attrs); err != nil {
		t.Fatalf("decoding object value: %s", err)
	}
	out := make(map[string]tftypes.Value, len(attrs))
	for name, value := range attrs {
		out[name] = value
	}
	return out
}

// macOnlyConfig is the configuration `resource "unifi_device" "pdu" { mac = … }`:
// every attribute null, and both nested blocks as the empty collections HCL
// produces for a block-less body.
func macOnlyConfig(objType tftypes.Object) map[string]tftypes.Value {
	config := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		config[name] = tftypes.NewValue(attrType, nil)
	}
	config["mac"] = tftypes.NewValue(tftypes.String, pduTestMAC)
	config["port_override"] = tftypes.NewValue(
		objType.AttributeTypes["port_override"], []tftypes.Value{})
	config["ethernet_override"] = tftypes.NewValue(
		objType.AttributeTypes["ethernet_override"], []tftypes.Value{})
	return config
}

// planDevice runs the provider's PlanResourceChange for unifi_device the way
// Terraform would, and returns the planned state value.
func planDevice(
	t *testing.T,
	objType tftypes.Object,
	prior, config, proposed tftypes.Value,
) tftypes.Value {
	t.Helper()
	ctx := context.Background()
	server := providerserver.NewProtocol6(New())()

	dv := func(v tftypes.Value) *tfprotov6.DynamicValue {
		enc, err := tfprotov6.NewDynamicValue(objType, v)
		if err != nil {
			t.Fatalf("encoding dynamic value: %s", err)
		}
		return &enc
	}

	resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "unifi_device",
		PriorState:       dv(prior),
		Config:           dv(config),
		ProposedNewState: dv(proposed),
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %s", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan diagnostic: %s: %s", d.Summary, d.Detail)
		}
	}
	planned, err := resp.PlannedState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshalling planned state: %s", err)
	}
	return planned
}

// changedAttributes lists the top-level attributes whose planned value differs
// from prior state, i.e. what Terraform would render as an in-place update.
func changedAttributes(
	t *testing.T,
	objType tftypes.Object,
	prior, planned tftypes.Value,
) []string {
	t.Helper()
	priorAttrs := map[string]tftypes.Value{}
	if err := prior.As(&priorAttrs); err != nil {
		t.Fatalf("decoding prior state: %s", err)
	}
	plannedAttrs := map[string]tftypes.Value{}
	if err := planned.As(&plannedAttrs); err != nil {
		t.Fatalf("decoding planned state: %s", err)
	}

	var changed []string
	for name := range objType.AttributeTypes {
		if priorAttrs[name].Equal(plannedAttrs[name]) {
			continue
		}
		changed = append(changed, name)
		t.Logf("changed %-20s prior=%.70s planned=%.70s",
			name, priorAttrs[name].String(), plannedAttrs[name].String())
	}
	return changed
}

// TestImportedDeviceMACOnlyPlansNoOp is the regression test for the spurious
// in-place update an imported USPPDUP planned from a MAC-only configuration.
// The trigger was never a controller field: it was the nested block
// collections, which Read stored as null while a block-less configuration
// arrives as an empty collection.
func TestImportedDeviceMACOnlyPlansNoOp(t *testing.T) {
	ctx := context.Background()
	model := refreshedPDUModel(t)
	prior := stateFromModel(t, model)
	objType, ok := prior.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}

	priorAttrs := copyObjectAttributes(t, prior.Raw)
	config := macOnlyConfig(objType)

	// Terraform keeps the prior value for every null-config attribute and takes
	// the configuration value verbatim for nested blocks
	// (objchange.proposedNewNestingList / proposedNewNestingSet).
	proposed := map[string]tftypes.Value{}
	for name, v := range priorAttrs {
		proposed[name] = v
	}
	proposed["port_override"] = config["port_override"]
	proposed["ethernet_override"] = config["ethernet_override"]

	planned := planDevice(t, objType, prior.Raw,
		tftypes.NewValue(objType, config), tftypes.NewValue(objType, proposed))

	if changed := changedAttributes(t, objType, prior.Raw, planned); len(changed) > 0 {
		t.Errorf("MAC-only configuration planned an update to %v, want no-op", changed)
	}
}

// TestNullBlockCollectionsPlanAnUpdate pins the mechanism down: put the old
// null-valued block collections back into state and the same MAC-only
// configuration plans an update again.
func TestOutletOverridesRemainUnknownDuringUpdate(t *testing.T) {
	ctx := context.Background()
	prior := stateFromModel(t, refreshedPDUModel(t))
	objType, ok := prior.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}

	config := macOnlyConfig(objType)
	config["name"] = tftypes.NewValue(tftypes.String, "renamed")
	proposed := copyObjectAttributes(t, prior.Raw)
	proposed["name"] = config["name"]
	proposed["outlet_enabled"] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
	proposed["outlet_overrides"] = tftypes.NewValue(
		objType.AttributeTypes["outlet_overrides"], tftypes.UnknownValue)

	planned := planDevice(t, objType, prior.Raw,
		tftypes.NewValue(objType, config), tftypes.NewValue(objType, proposed))
	attrs := copyObjectAttributes(t, planned)
	if attrs["outlet_enabled"].IsKnown() || attrs["outlet_overrides"].IsKnown() {
		t.Errorf("outlet state was pinned during update: enabled=%s overrides=%s",
			attrs["outlet_enabled"], attrs["outlet_overrides"])
	}
}

func TestNullBlockCollectionsPlanAnUpdate(t *testing.T) {
	ctx := context.Background()
	model := refreshedPDUModel(t)
	model.PortOverride = types.SetNull(types.ObjectType{AttrTypes: portOverrideAttrTypes()})
	model.EthernetOverride = types.ListNull(ethernetOverrideObjectType())
	prior := stateFromModel(t, model)
	objType, ok := prior.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}

	priorAttrs := copyObjectAttributes(t, prior.Raw)
	config := macOnlyConfig(objType)
	proposed := map[string]tftypes.Value{}
	for name, v := range priorAttrs {
		proposed[name] = v
	}
	proposed["port_override"] = config["port_override"]
	proposed["ethernet_override"] = config["ethernet_override"]

	planned := planDevice(t, objType, prior.Raw,
		tftypes.NewValue(objType, config), tftypes.NewValue(objType, proposed))

	changed := changedAttributes(t, objType, prior.Raw, planned)
	if len(changed) != 2 {
		t.Fatalf("null block collections changed %v, want port_override and ethernet_override",
			changed)
	}
}

// TestRefreshBlockStateIsNeverNull states the invariant directly: whatever the
// prior value, the refreshed block collections are known and non-null.
func TestRefreshBlockStateIsNeverNull(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}
	portType := types.ObjectType{AttrTypes: portOverrideAttrTypes()}

	for _, tc := range []struct {
		name  string
		ports types.Set
		eths  types.List
	}{
		{"null", types.SetNull(portType), types.ListNull(ethernetOverrideObjectType())},
		{"unknown", types.SetUnknown(portType), types.ListUnknown(ethernetOverrideObjectType())},
		{
			"empty",
			types.SetValueMust(portType, nil),
			types.ListValueMust(ethernetOverrideObjectType(), nil),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ports, diags := r.refreshPortOverrideState(ctx, tc.ports, nil)
			if diags.HasError() {
				t.Fatalf("port diagnostics: %v", diags)
			}
			if ports.IsNull() || ports.IsUnknown() || len(ports.Elements()) != 0 {
				t.Errorf("port_override = %s, want empty set", ports)
			}

			eths, diags := refreshEthernetOverrideState(ctx, tc.eths, nil)
			if diags.HasError() {
				t.Fatalf("ethernet diagnostics: %v", diags)
			}
			if eths.IsNull() || eths.IsUnknown() || len(eths.Elements()) != 0 {
				t.Errorf("ethernet_override = %s, want empty list", eths)
			}
		})
	}
}

// TestRefreshEthernetOverrideStateKeepsDeclaredInterfaces guards the partial
// ownership behavior: a declared interface still refreshes from the device.
func TestRefreshEthernetOverrideStateKeepsDeclaredInterfaces(t *testing.T) {
	ctx := context.Background()
	prior := types.ListValueMust(ethernetOverrideObjectType(), []attr.Value{
		types.ObjectValueMust(ethernetOverrideAttrTypes(), map[string]attr.Value{
			"ifname":        types.StringValue("eth3"),
			"network_group": types.StringValue("LAN"),
		}),
	})

	refreshed, diags := refreshEthernetOverrideState(ctx, prior, []unifi.DeviceEthernetOverrides{
		{Ifname: "eth0", NetworkGroup: "WAN"},
		{Ifname: "eth3", NetworkGroup: "LAN2"},
	})
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}

	var models []ethernetOverrideModel
	if d := refreshed.ElementsAs(ctx, &models, false); d.HasError() {
		t.Fatalf("ElementsAs diagnostics: %v", d)
	}
	if len(models) != 1 || models[0].Ifname.ValueString() != "eth3" ||
		models[0].NetworkGroup.ValueString() != "LAN2" {
		t.Errorf("refreshed = %v, want only eth3 with the device's LAN2 group", models)
	}
}

func TestPlanChangesOnlyAdoptionFlags(t *testing.T) {
	ctx := context.Background()
	base := refreshedPDUModel(t)
	state := stateFromModel(t, base)
	objType, ok := state.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}

	planWith := func(mutate func(map[string]tftypes.Value)) tfsdk.Plan {
		attrs := copyObjectAttributes(t, state.Raw)
		mutate(attrs)
		return tfsdk.Plan{Schema: state.Schema, Raw: tftypes.NewValue(objType, attrs)}
	}
	configWith := func(mutate func(map[string]tftypes.Value)) tfsdk.Config {
		attrs := macOnlyConfig(objType)
		mutate(attrs)
		return tfsdk.Config{Schema: state.Schema, Raw: tftypes.NewValue(objType, attrs)}
	}
	baseConfig := configWith(func(map[string]tftypes.Value) {})

	tests := []struct {
		name   string
		plan   tfsdk.Plan
		config tfsdk.Config
		expect bool
	}{
		{
			name:   "no change at all",
			plan:   planWith(func(map[string]tftypes.Value) {}),
			expect: false,
		},
		{
			name: "only forget_on_destroy",
			plan: planWith(func(a map[string]tftypes.Value) {
				a["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, false)
			}),
			expect: true,
		},
		{
			name: "flag change with computed attributes unknown",
			plan: planWith(func(a map[string]tftypes.Value) {
				a["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, false)
				a["adopted"] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
				a["state"] = tftypes.NewValue(tftypes.Number, tftypes.UnknownValue)
				a["outlet_enabled"] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
			}),
			expect: true,
		},
		{
			name: "flag change alongside a real field",
			plan: planWith(func(a map[string]tftypes.Value) {
				a["allow_adoption"] = tftypes.NewValue(tftypes.Bool, false)
				a["name"] = tftypes.NewValue(tftypes.String, "renamed")
			}),
			expect: false,
		},
		{
			name: "flag change with configured unknown field",
			plan: planWith(func(a map[string]tftypes.Value) {
				a["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, false)
				a["name"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
			}),
			config: configWith(func(a map[string]tftypes.Value) {
				a["name"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
			}),
			expect: false,
		},
		{
			name: "unknown flag",
			plan: planWith(func(a map[string]tftypes.Value) {
				a["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
			}),
			expect: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := tc.config
			if config.Raw.IsNull() {
				config = baseConfig
			}
			got, diags := planChangesOnlyAdoptionFlags(tc.plan, state, config)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got != tc.expect {
				t.Errorf("planChangesOnlyAdoptionFlags() = %v, want %v", got, tc.expect)
			}
		})
	}
}

// TestUpdateFlagOnlyChangeSkipsController proves the short-circuit never
// touches the controller: the resource holds a client whose API handle is nil,
// so any request would panic before it could reach the network.
func TestUpdateFlagOnlyChangeSkipsController(t *testing.T) {
	ctx := context.Background()
	base := refreshedPDUModel(t)
	state := stateFromModel(t, base)
	objType, ok := state.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}

	attrs := copyObjectAttributes(t, state.Raw)
	attrs["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, false)
	plan := tfsdk.Plan{Schema: state.Schema, Raw: tftypes.NewValue(objType, attrs)}
	configAttrs := macOnlyConfig(objType)
	configAttrs["forget_on_destroy"] = tftypes.NewValue(tftypes.Bool, false)
	config := tfsdk.Config{Schema: state.Schema, Raw: tftypes.NewValue(objType, configAttrs)}

	r := &deviceResource{client: &Client{Site: "default"}}
	identityResp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, identityResp)
	identityType := identityResp.IdentitySchema.Type().TerraformType(ctx)

	resp := &fwresource.UpdateResponse{
		State: tfsdk.State{Schema: state.Schema, Raw: state.Raw.Copy()},
		Identity: &tfsdk.ResourceIdentity{
			Schema: identityResp.IdentitySchema,
			Raw:    tftypes.NewValue(identityType, nil),
		},
	}
	r.Update(ctx, fwresource.UpdateRequest{Plan: plan, State: state, Config: config}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", resp.Diagnostics)
	}

	var updated deviceResourceModel
	if d := resp.State.Get(ctx, &updated); d.HasError() {
		t.Fatalf("reading updated state: %v", d)
	}
	if updated.ForgetOnDestroy.ValueBool() {
		t.Errorf("forget_on_destroy = true, want the planned false")
	}
	if !updated.AllowAdoption.ValueBool() {
		t.Errorf("allow_adoption = false, want the unchanged true")
	}
	if updated.Name.ValueString() != "USP PDU Pro" {
		t.Errorf("name = %q, want the prior controller value", updated.Name.ValueString())
	}

	var identityMAC hwtypes.MACAddress
	if d := resp.Identity.GetAttribute(ctx, path.Root("mac"), &identityMAC); d.HasError() {
		t.Fatalf("reading identity: %v", d)
	}
	if identityMAC.ValueString() != pduTestMAC {
		t.Errorf("identity mac = %q, want %q", identityMAC.ValueString(), pduTestMAC)
	}
}
