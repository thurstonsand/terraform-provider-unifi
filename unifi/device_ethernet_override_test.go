package unifi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// liveUDMEthernetOverrides mirrors the ethernet_overrides list a UDM running
// Network 10.5.67 returns from stat/device: 11 entries, controller order,
// `disabled` absent, eth8 on WAN and eth9 on WAN2.
func liveUDMEthernetOverrides() []unifi.DeviceEthernetOverrides {
	return []unifi.DeviceEthernetOverrides{
		{Ifname: "eth0", NetworkGroup: "LAN"},
		{Ifname: "eth9", NetworkGroup: "WAN2"},
		{Ifname: "eth10", NetworkGroup: "LAN"},
		{Ifname: "eth1", NetworkGroup: "LAN"},
		{Ifname: "eth2", NetworkGroup: "LAN"},
		{Ifname: "eth3", NetworkGroup: "LAN"},
		{Ifname: "eth4", NetworkGroup: "LAN"},
		{Ifname: "eth5", NetworkGroup: "LAN"},
		{Ifname: "eth6", NetworkGroup: "LAN"},
		{Ifname: "eth7", NetworkGroup: "LAN"},
		{Ifname: "eth8", NetworkGroup: "WAN"},
	}
}

func ethernetOverrideList(t *testing.T, entries ...[2]string) types.List {
	t.Helper()
	models := make([]ethernetOverrideModel, 0, len(entries))
	for _, e := range entries {
		models = append(models, ethernetOverrideModel{
			Ifname:       types.StringValue(e[0]),
			NetworkGroup: types.StringValue(e[1]),
		})
	}
	list, diags := types.ListValueFrom(
		context.Background(), ethernetOverrideObjectType(), models)
	if diags.HasError() {
		t.Fatalf("building ethernet_override list: %v", diags)
	}
	return list
}

func TestEthernetOverrideSchema(t *testing.T) {
	ctx := context.Background()
	var resp fwresource.SchemaResponse
	(&deviceResource{}).Schema(ctx, fwresource.SchemaRequest{}, &resp)

	block, ok := resp.Schema.Blocks["ethernet_override"].(schema.ListNestedBlock)
	if !ok {
		t.Fatalf(
			"ethernet_override is not a ListNestedBlock: %T",
			resp.Schema.Blocks["ethernet_override"],
		)
	}

	attrs := block.NestedObject.Attributes
	if len(attrs) != 2 {
		t.Errorf(
			"ethernet_override exposes %d attributes, want exactly ifname and network_group: %v",
			len(attrs), attrs,
		)
	}
	for _, name := range []string{"ifname", "network_group"} {
		attr, ok := attrs[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("ethernet_override.%s is not a StringAttribute: %T", name, attrs[name])
		}
		if !attr.Required {
			t.Errorf("ethernet_override.%s must be Required", name)
		}
		if len(attr.Validators) == 0 {
			t.Errorf("ethernet_override.%s has no validator", name)
		}
	}

	for _, name := range []string{"speed", "disabled", "relay", "full_duplex"} {
		if _, present := attrs[name]; present {
			t.Errorf("ethernet_override must not expose %q", name)
		}
	}

	for _, ifname := range []string{"eth0", "eth8", "eth10", "eth99"} {
		if !ethernetIfnamePattern.MatchString(ifname) {
			t.Errorf("ifname pattern rejects %q", ifname)
		}
	}
	for _, ifname := range []string{"eth", "eth100", "Eth8", "wan0", "eth8 "} {
		if ethernetIfnamePattern.MatchString(ifname) {
			t.Errorf("ifname pattern accepts %q", ifname)
		}
	}
}

func TestResolveEthernetOverridesForUpdate(t *testing.T) {
	live := liveUDMEthernetOverrides()

	t.Run("two-entry overlay preserves order and untouched entries", func(t *testing.T) {
		declared := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "WAN"},
			{Ifname: "eth9", NetworkGroup: "WAN2"},
		}
		got, diags := resolveEthernetOverridesForUpdate(live, declared)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if len(got) != 11 {
			t.Fatalf("resolved length = %d, want 11: %+v", len(got), got)
		}
		for i := range live {
			if got[i].Ifname != live[i].Ifname {
				t.Fatalf(
					"controller order not preserved at %d: got %q, want %q",
					i, got[i].Ifname, live[i].Ifname,
				)
			}
			if got[i].NetworkGroup != live[i].NetworkGroup {
				t.Errorf(
					"%s network group = %q, want %q (declared entries match live)",
					got[i].Ifname, got[i].NetworkGroup, live[i].NetworkGroup,
				)
			}
		}
	})

	t.Run("reassignment touches only the declared interface", func(t *testing.T) {
		declared := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth9", NetworkGroup: "LAN"},
		}
		got, diags := resolveEthernetOverridesForUpdate(live, declared)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if got[1].Ifname != "eth9" || got[1].NetworkGroup != "LAN" {
			t.Errorf("eth9 = %+v, want LAN in place", got[1])
		}
		if got[10].Ifname != "eth8" || got[10].NetworkGroup != "WAN" {
			t.Errorf("undeclared eth8 was altered: %+v", got[10])
		}
	})

	t.Run("unmanaged fields survive the overlay", func(t *testing.T) {
		current := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "LAN", Disabled: true},
			{Ifname: "eth9", NetworkGroup: "LAN", Disabled: true},
		}
		declared := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "WAN"},
		}
		got, diags := resolveEthernetOverridesForUpdate(current, declared)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if !got[0].Disabled {
			t.Errorf("declared entry lost Disabled: %+v", got[0])
		}
		if got[0].NetworkGroup != "WAN" {
			t.Errorf("eth8 network group = %q, want WAN", got[0].NetworkGroup)
		}
		if !got[1].Disabled || got[1].NetworkGroup != "LAN" {
			t.Errorf("undeclared entry altered: %+v", got[1])
		}
		if current[0].NetworkGroup != "LAN" {
			t.Errorf("the live slice was mutated in place: %+v", current[0])
		}
	})

	t.Run("no declared blocks leaves the field unmanaged", func(t *testing.T) {
		got, diags := resolveEthernetOverridesForUpdate(live, nil)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if got != nil {
			t.Errorf("resolved = %+v, want nil so the field stays off the wire", got)
		}
	})

	t.Run("relinquishing ownership preserves live assignments", func(t *testing.T) {
		// The practitioner deleted every ethernet_override block: the model
		// converts to a nil list, so nothing is sent and the controller keeps
		// eth8/WAN and eth9/WAN2.
		got, diags := resolveEthernetOverridesForUpdate(live, nil)
		if diags.HasError() || got != nil {
			t.Fatalf("resolved = %+v, diags = %v; want nil and no error", got, diags)
		}
		body := buildMinimalUpdateDevice(
			&unifi.Device{ID: "d1", EthernetOverrides: got}, nil, nil)
		if body.EthernetOverrides != nil {
			t.Errorf("PUT body carries %+v, want no ethernet_overrides key", body.EthernetOverrides)
		}
	})

	t.Run("duplicate declared interface fails", func(t *testing.T) {
		declared := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "WAN"},
			{Ifname: "eth8", NetworkGroup: "LAN"},
		}
		got, diags := resolveEthernetOverridesForUpdate(live, declared)
		if !diags.HasError() {
			t.Fatalf("duplicate ifname accepted: %+v", got)
		}
		assertNoDevicePayload(t, diags.Errors()[0].Detail())
	})

	t.Run("duplicate live interface fails", func(t *testing.T) {
		current := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "WAN"},
			{Ifname: "eth8", NetworkGroup: "LAN"},
		}
		declared := []unifi.DeviceEthernetOverrides{{Ifname: "eth8", NetworkGroup: "WAN"}}
		got, diags := resolveEthernetOverridesForUpdate(current, declared)
		if !diags.HasError() {
			t.Fatalf("ambiguous live list accepted: %+v", got)
		}
		assertNoDevicePayload(t, diags.Errors()[0].Detail())
	})

	t.Run("interface absent from the device fails", func(t *testing.T) {
		declared := []unifi.DeviceEthernetOverrides{{Ifname: "eth11", NetworkGroup: "WAN"}}
		got, diags := resolveEthernetOverridesForUpdate(live, declared)
		if !diags.HasError() {
			t.Fatalf("unknown interface accepted, provider invented one: %+v", got)
		}
		detail := diags.Errors()[0].Detail()
		if !strings.Contains(detail, "eth11") {
			t.Errorf("diagnostic does not name the offending interface: %q", detail)
		}
		assertNoDevicePayload(t, detail)
	})

	t.Run("empty live list rejects any declared interface", func(t *testing.T) {
		declared := []unifi.DeviceEthernetOverrides{{Ifname: "eth8", NetworkGroup: "WAN"}}
		if _, diags := resolveEthernetOverridesForUpdate(nil, declared); !diags.HasError() {
			t.Error("declared interface accepted against a device with no ethernet_overrides")
		}
	})
}

// assertNoDevicePayload keeps diagnostics from leaking the whole device: a
// serialized payload is the failure mode we guard against, so the detail must
// stay short and free of device-level keys.
func assertNoDevicePayload(t *testing.T, detail string) {
	t.Helper()
	for _, leak := range []string{"port_overrides", "\"mac\"", "config_network", "x_authkey"} {
		if strings.Contains(detail, leak) {
			t.Errorf("diagnostic leaks device payload (%q): %s", leak, detail)
		}
	}
	if len(detail) > 500 {
		t.Errorf("diagnostic is %d chars, too long to be anything but a payload dump", len(detail))
	}
}

func TestRefreshEthernetOverrides(t *testing.T) {
	ctx := context.Background()

	t.Run("null prior stays unmanaged", func(t *testing.T) {
		prior := types.ListNull(ethernetOverrideObjectType())
		got, diags := refreshEthernetOverrides(ctx, prior, liveUDMEthernetOverrides())
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if !got.IsNull() {
			t.Errorf("refreshed = %v, want null", got)
		}
	})

	t.Run("keeps the declared subset and shows drift", func(t *testing.T) {
		prior := ethernetOverrideList(t,
			[2]string{"eth8", "WAN"},
			[2]string{"eth9", "WAN2"},
		)
		live := liveUDMEthernetOverrides()
		live[1].NetworkGroup = "LAN" // eth9 reassigned on the controller

		got, diags := refreshEthernetOverrides(ctx, prior, live)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}

		var models []ethernetOverrideModel
		if d := got.ElementsAs(ctx, &models, false); d.HasError() {
			t.Fatalf("reading refreshed list: %v", d)
		}
		if len(models) != 2 {
			t.Fatalf("refreshed %d entries, want the 2 declared ones: %+v", len(models), models)
		}
		if models[0].Ifname.ValueString() != "eth8" ||
			models[0].NetworkGroup.ValueString() != "WAN" {
			t.Errorf("eth8 = %+v, want WAN", models[0])
		}
		if models[1].NetworkGroup.ValueString() != "LAN" {
			t.Errorf(
				"eth9 network group = %q, want LAN so the controller drift is visible",
				models[1].NetworkGroup.ValueString(),
			)
		}
	})

	t.Run("interface missing from the device fails refresh", func(t *testing.T) {
		prior := ethernetOverrideList(t, [2]string{"eth11", "WAN"})
		_, diags := refreshEthernetOverrides(ctx, prior, liveUDMEthernetOverrides())
		if !diags.HasError() {
			t.Fatal("missing configured interface did not fail refresh")
		}
		if !strings.Contains(diags.Errors()[0].Detail(), "eth11") {
			t.Errorf("diagnostic does not name the missing interface: %v", diags)
		}
	})

	t.Run("duplicate live interface fails refresh", func(t *testing.T) {
		prior := ethernetOverrideList(t, [2]string{"eth8", "WAN"})
		live := liveUDMEthernetOverrides()
		live = append(live, live[10])
		_, diags := refreshEthernetOverrides(ctx, prior, live)
		if !diags.HasError() {
			t.Fatal("ambiguous live interface did not fail refresh")
		}
	})
}

func TestFrameworkToEthernetOverrides(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}

	t.Run("null list is unmanaged", func(t *testing.T) {
		got, diags := r.frameworkToEthernetOverrides(
			ctx, types.ListNull(ethernetOverrideObjectType()))
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if got != nil {
			t.Errorf("converted = %+v, want nil", got)
		}
	})

	t.Run("declared blocks convert in order", func(t *testing.T) {
		list := ethernetOverrideList(t,
			[2]string{"eth8", "WAN"},
			[2]string{"eth9", "WAN2"},
		)
		got, diags := r.frameworkToEthernetOverrides(ctx, list)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		want := []unifi.DeviceEthernetOverrides{
			{Ifname: "eth8", NetworkGroup: "WAN"},
			{Ifname: "eth9", NetworkGroup: "WAN2"},
		}
		if len(got) != len(want) {
			t.Fatalf("converted %d entries, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	// A config declaring no ethernet_override block arrives as an empty list,
	// not null, so empty must relinquish ownership just like null does.
	t.Run("empty block list is unmanaged", func(t *testing.T) {
		got, diags := r.frameworkToEthernetOverrides(ctx, ethernetOverrideList(t))
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if got != nil {
			t.Errorf("converted = %+v, want nil so the field stays unmanaged", got)
		}
	})
}

// A device resource that never declares an ethernet_override must produce the
// same PUT body it did before this field existed.
func TestModelToAPIDeviceLeavesEthernetOverridesUnmanaged(t *testing.T) {
	ctx := context.Background()
	r := &deviceResource{}

	model := &deviceResourceModel{
		MAC:  hwtypes.NewMACAddressValue("00:11:22:33:44:55"),
		Name: types.StringValue("udm"),
	}
	device, diags := r.modelToAPIDevice(ctx, model)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if device.EthernetOverrides != nil {
		t.Fatalf("undeclared ethernet_override produced %+v, want nil", device.EthernetOverrides)
	}

	resolved, resolveDiags := resolveEthernetOverridesForUpdate(
		liveUDMEthernetOverrides(), device.EthernetOverrides)
	if resolveDiags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resolveDiags)
	}
	device.EthernetOverrides = resolved

	raw, err := json.Marshal(buildMinimalUpdateDevice(device, nil, nil))
	if err != nil {
		t.Fatalf("marshalling body: %v", err)
	}
	if strings.Contains(string(raw), "ethernet_overrides") {
		t.Errorf("a no-op device resource started sending ethernet_overrides: %s", raw)
	}
}

func TestBuildMinimalUpdateDeviceEthernetOverrides(t *testing.T) {
	t.Run("managed overrides travel in the PUT", func(t *testing.T) {
		req := &unifi.Device{
			ID:                "d1",
			EthernetOverrides: liveUDMEthernetOverrides(),
		}
		body := buildMinimalUpdateDevice(req, nil, nil)
		if len(body.EthernetOverrides) != 11 {
			t.Fatalf(
				"PUT body carries %d ethernet_overrides, want the full 11-entry list",
				len(body.EthernetOverrides),
			)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshalling body: %v", err)
		}
		if !strings.Contains(string(raw), `"ethernet_overrides"`) {
			t.Errorf("ethernet_overrides missing from body: %s", raw)
		}
	})

	t.Run("unmanaged overrides stay off the wire", func(t *testing.T) {
		body := buildMinimalUpdateDevice(&unifi.Device{ID: "d1"}, nil, nil)
		if body.EthernetOverrides != nil {
			t.Errorf("PUT body carries %+v, want nil", body.EthernetOverrides)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshalling body: %v", err)
		}
		if strings.Contains(string(raw), "ethernet_overrides") {
			t.Errorf("an unmanaged field reached the body: %s", raw)
		}
	})
}
