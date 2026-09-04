package unifi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestDeviceOutletAttributesAreObservationOnly checks that no HCL can express an
// outlet change: not the top-level toggle, not a relay state, not a power cycle.
// The PDU these attributes describe powers the network's core equipment.
func TestDeviceOutletAttributesAreObservationOnly(t *testing.T) {
	r := &deviceResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	assertObservationOnly := func(name string, attr schema.Attribute) {
		t.Helper()
		if attr.IsOptional() || attr.IsRequired() {
			t.Errorf("%s is configurable (optional=%v required=%v), want computed only",
				name, attr.IsOptional(), attr.IsRequired())
		}
		if !attr.IsComputed() {
			t.Errorf("%s is not computed", name)
		}
	}

	outletEnabled, ok := resp.Schema.Attributes["outlet_enabled"]
	if !ok {
		t.Fatal("outlet_enabled is missing from the schema")
	}
	assertObservationOnly("outlet_enabled", outletEnabled)

	outletOverrides, ok := resp.Schema.Attributes["outlet_overrides"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("outlet_overrides is %T, want a ListNestedAttribute", resp.Schema.Attributes["outlet_overrides"])
	}
	assertObservationOnly("outlet_overrides", outletOverrides)
	if len(outletOverrides.PlanModifiers) != 0 {
		t.Errorf("outlet_overrides has %d plan modifiers, want none so live outlet changes remain valid during apply", len(outletOverrides.PlanModifiers))
	}

	nested := outletOverrides.NestedObject.Attributes
	for _, name := range []string{"index", "name", "relay_state", "cycle_enabled"} {
		attr, ok := nested[name]
		if !ok {
			t.Errorf("outlet_overrides.%s is missing from the schema", name)
			continue
		}
		assertObservationOnly("outlet_overrides."+name, attr)
	}
}

// TestUpdateBodyOmitsOutletFields drives an unrelated change (a rename) through
// the same conversion path an update takes, with outlet state present in both
// the model and the controller's device, and checks the wire body carries no
// outlet key at all.
func TestUpdateBodyOmitsOutletFields(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}
	currentDevice := sanitizedPDUDevice()

	model := refreshedPDUModel(t)
	model.Name = types.StringValue("renamed pdu")

	deviceReq, diags := r.modelToAPIDevice(ctx, &model)
	if diags.HasError() {
		t.Fatalf("modelToAPIDevice diagnostics: %v", diags)
	}
	if deviceReq.OutletEnabled || deviceReq.OutletOverrides != nil {
		t.Errorf("modelToAPIDevice carried outlet state: enabled=%v overrides=%v",
			deviceReq.OutletEnabled, deviceReq.OutletOverrides)
	}

	deviceReq.ID = currentDevice.ID
	minimal := buildMinimalUpdateDevice(deviceReq, currentDevice,
		resolvePortOverridesForUpdate(currentDevice, deviceReq))

	body, err := json.Marshal(minimal)
	if err != nil {
		t.Fatalf("marshalling update body: %s", err)
	}

	for _, key := range []string{
		"outlet_enabled",
		"outlet_overrides",
		"relay_state",
		"cycle_enabled",
	} {
		if strings.Contains(string(body), key) {
			t.Errorf("update body contains %q: %s", key, body)
		}
	}
	if !strings.Contains(string(body), `"name":"renamed pdu"`) {
		t.Errorf("update body lost the rename: %s", body)
	}
}

// TestOutletOverridesToFrameworkStillReads keeps the observation half honest:
// the controller's outlet list must still land in state.
func TestOutletOverridesToFrameworkStillReads(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}
	var diags diag.Diagnostics

	model := deviceResourceModel{Timeouts: nullTimeouts()}
	r.setResourceData(ctx, &diags, sanitizedPDUDevice(), &model, "default")
	if diags.HasError() {
		t.Fatalf("setResourceData diagnostics: %v", diags)
	}

	if !model.OutletEnabled.ValueBool() {
		t.Error("outlet_enabled was not read from the device")
	}

	var outlets []outletOverrideModel
	if d := model.OutletOverrides.ElementsAs(ctx, &outlets, false); d.HasError() {
		t.Fatalf("ElementsAs diagnostics: %v", d)
	}
	if len(outlets) != 2 {
		t.Fatalf("read %d outlets, want 2", len(outlets))
	}
	if !outlets[0].RelayState.ValueBool() || outlets[0].Name.ValueString() != "Outlet 1" {
		t.Errorf("outlet 1 = %+v, want the controller's values", outlets[0])
	}
}
