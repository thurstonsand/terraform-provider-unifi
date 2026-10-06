package unifi

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccWANFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.test", "id"),
					resource.TestCheckResourceAttr("unifi_wan.test", "name", "test-wan"),
					resource.TestCheckResourceAttr("unifi_wan.test", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.test", "vlan.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_wan.test", "vlan.id", "10"),
					resource.TestCheckResourceAttr("unifi_wan.test", "enabled", "true"),
				),
			},
			{
				ResourceName:      "unifi_wan.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Identity-based import (import block with identity, Terraform 1.12+).
			{
				ResourceName:    "unifi_wan.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

// TestAccWANFramework_minimal verifies that a WAN with no optional nested objects
// can be created and imported without "was null, but now..." errors from API defaults.
func TestAccWANFramework_minimal(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_minimal(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.minimal", "id"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "name", "test-wan-minimal"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "enabled", "true"),
				),
			},
			{
				ResourceName:      "unifi_wan.minimal",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccWANFramework_withNestedObjects verifies that explicitly configured nested
// objects are preserved through create, read, and import.
func TestAccWANFramework_withNestedObjects(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_withNestedObjects(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.nested", "id"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "name", "test-wan-nested"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "enabled", "true"),
					// VLAN
					resource.TestCheckResourceAttr("unifi_wan.nested", "vlan.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "vlan.id", "20"),
					// DNS
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.preference", "manual"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.primary", "8.8.8.8"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.secondary", "8.8.4.4"),
					// Load Balance
					resource.TestCheckResourceAttrSet(
						"unifi_wan.nested",
						"load_balance.failover_priority",
					),
				),
			},
			{
				ResourceName:      "unifi_wan.nested",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccWANFrameworkConfig_basic() string {
	return `
resource "unifi_wan" "test" {
	name    = "test-wan"
	type    = "dhcp"
	enabled = true

	vlan = {
		enabled = true
		id      = 10
	}
}
`
}

func testAccWANFrameworkConfig_minimal() string {
	return `
resource "unifi_wan" "minimal" {
	name    = "test-wan-minimal"
	type    = "dhcp"
	enabled = true
}
`
}

// TestAccWANFramework_macOverrideLifecycle walks the clone through set, import
// and removal. The removal step is the one that used to be impossible: with
// mac_override Optional+Computed the prior value was replanned out of state and
// the controller kept the clone forever.
func TestAccWANFramework_macOverrideLifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_macOverride(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_wan.mac", "mac_override", "02:00:00:00:00:01"),
					resource.TestCheckResourceAttr("unifi_wan.mac", "mac_override_enabled", "true"),
				),
			},
			{
				ResourceName:      "unifi_wan.mac",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccWANFrameworkConfig_macOverride(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("unifi_wan.mac", "mac_override"),
					resource.TestCheckResourceAttr("unifi_wan.mac", "mac_override_enabled", "false"),
				),
			},
			// The clone is gone on the controller too, so the removal converges
			// instead of coming back as drift on the next refresh.
			{
				Config:   testAccWANFrameworkConfig_macOverride(false),
				PlanOnly: true,
			},
		},
	})
}

func testAccWANFrameworkConfig_macOverride(withMAC bool) string {
	mac := ""
	if withMAC {
		mac = `
	mac_override         = "02:00:00:00:00:01"
	mac_override_enabled = true
`
	}
	return `
resource "unifi_wan" "mac" {
	name    = "test-wan-mac"
	type    = "dhcp"
	enabled = true
` + mac + `}
`
}

func testAccWANFrameworkConfig_withNestedObjects() string {
	return `
resource "unifi_wan" "nested" {
	name    = "test-wan-nested"
	type    = "dhcp"
	enabled = true

	vlan = {
		enabled = true
		id      = 20
	}

	dns = {
		preference = "manual"
		primary    = "8.8.8.8"
		secondary  = "8.8.4.4"
	}

	load_balance = {
		failover_priority = 1
	}
}
`
}

// TestAccWANFramework_additionalFields verifies the newly exposed top-level
// fields round-trip through create, read, and import without spurious diffs.
func TestAccWANFramework_additionalFields(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_additionalFields(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.extra", "id"),
					resource.TestCheckResourceAttr("unifi_wan.extra", "name", "test-wan-extra"),
					// Computed fields populated from the controller.
					resource.TestCheckResourceAttrSet(
						"unifi_wan.extra",
						"mac_override_enabled",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_wan.extra",
						"wan_dslite_remote_host_auto",
					),
				),
			},
			{
				ResourceName:      "unifi_wan.extra",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccWANFrameworkConfig_additionalFields() string {
	// Note: setting_preference is intentionally NOT pinned here. The controller
	// treats it as a managed/derived field on WAN networks and reverts it to
	// "auto" for a dhcp WAN regardless of what we send (even with manual DNS),
	// which makes "manual" produce perpetual auto->manual plan drift. The other
	// newly exposed top-level fields below do round-trip cleanly.
	return `
resource "unifi_wan" "extra" {
	name    = "test-wan-extra"
	type    = "dhcp"
	enabled = true
}
`
}

func TestNewWANResource(t *testing.T) {
	got := NewWANResource()
	if got == nil {
		t.Fatal("NewWANResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("NewWANResource() does not implement fwresource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("NewWANResource() does not implement fwresource.ResourceWithIdentity")
	}
}

func TestNewWANListResource(t *testing.T) {
	got := NewWANListResource()
	if got == nil {
		t.Fatal("NewWANListResource() returned nil")
	}
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf("NewWANListResource() does not implement fwlist.ListResourceWithConfigure")
	}
}

func Test_vlanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    vlanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled": types.BoolType,
				"id":      types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("vlanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_egressQosModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    egressQosModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":  types.BoolType,
				"priority": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("egressQosModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_smartqModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    smartqModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":   types.BoolType,
				"up_rate":   types.Int64Type,
				"down_rate": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("smartqModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_providerCapabilitiesModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    providerCapabilitiesModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"download_kilobits_per_second": types.Int64Type,
				"upload_kilobits_per_second":   types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("providerCapabilitiesModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpOptionModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpOptionModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"option_number": types.Int64Type,
				"value":         types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpOptionModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_wanResource_networkGroup guards #334: the WAN network group must be
// preserved in the update PUT (and attr_hidden_id mirror it), instead of being
// hard-coded to "WAN" — otherwise a secondary uplink (WAN2) collides with the
// primary and the controller rejects it.
func Test_wanResource_networkGroup(t *testing.T) {
	r := &wanResource{}
	ctx := context.Background()

	base := wanResourceModel{
		Name:    types.StringValue("CC Internet SFP"),
		Enabled: types.BoolValue(true),
		Type:    types.StringValue("dhcp"),
	}

	t.Run("WAN2 is preserved and mirrored to hidden id", func(t *testing.T) {
		m := base
		m.NetworkGroup = types.StringValue("WAN2")
		n, d := r.modelToNetwork(ctx, &m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if n.WANNetworkGroup == nil || *n.WANNetworkGroup != "WAN2" {
			t.Errorf("WANNetworkGroup = %v, want WAN2", n.WANNetworkGroup)
		}
		if n.HiddenID != "WAN2" {
			t.Errorf("HiddenID = %q, want WAN2", n.HiddenID)
		}
	})

	t.Run("unset defaults to WAN", func(t *testing.T) {
		m := base
		m.NetworkGroup = types.StringNull()
		n, d := r.modelToNetwork(ctx, &m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if n.WANNetworkGroup == nil || *n.WANNetworkGroup != "WAN" {
			t.Errorf("WANNetworkGroup = %v, want WAN", n.WANNetworkGroup)
		}
		if n.HiddenID != "WAN" {
			t.Errorf("HiddenID = %q, want WAN", n.HiddenID)
		}
	})
}

// Test_wanResource_overlayConfig_dslite guards #281: the controller forces
// wan_dslite_remote_host_auto back to true server-side, so the API value in
// state would conflict with a user-configured false. overlayConfig must keep the
// user's planned value when it was set in config, and leave the controller value
// when it wasn't.
func Test_wanResource_overlayConfig_dslite(t *testing.T) {
	r := &wanResource{}

	t.Run("configured false overrides controller true", func(t *testing.T) {
		state := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		config := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		plan := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		r.overlayConfig(&state, &config, &plan)
		if state.DsliteRemoteHostAuto.ValueBool() {
			t.Errorf("DsliteRemoteHostAuto = true, want false (planned value)")
		}
	})

	t.Run("unset keeps controller value", func(t *testing.T) {
		state := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		config := wanResourceModel{DsliteRemoteHostAuto: types.BoolNull()}
		plan := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		r.overlayConfig(&state, &config, &plan)
		if !state.DsliteRemoteHostAuto.ValueBool() {
			t.Errorf("DsliteRemoteHostAuto = false, want true (controller value kept)")
		}
	})
}

// Test_dnsAddrValue guards #333: the controller persists an unset WAN DNS
// address as "" and returns it, but the Optional address fields plan as null.
// "" (and a nil pointer) must map to null so the post-apply read matches the
// plan; a real address must round-trip.
func Test_dnsAddrValue(t *testing.T) {
	empty := ""
	addr := "2001:4860:4860::8888"
	v4 := "8.8.8.8"

	cases := []struct {
		name     string
		in       *string
		wantNull bool
		wantStr  string
	}{
		{"nil pointer -> null", nil, true, ""},
		{"empty string -> null", &empty, true, ""},
		{"ipv6 address survives", &addr, false, addr},
		{"ipv4 address survives", &v4, false, v4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dnsAddrValue(c.in)
			if got.IsNull() != c.wantNull {
				t.Errorf("IsNull = %v, want %v", got.IsNull(), c.wantNull)
			}
			if !c.wantNull && got.ValueString() != c.wantStr {
				t.Errorf("ValueString = %q, want %q", got.ValueString(), c.wantStr)
			}
		})
	}
}

func Test_dnsModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dnsModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"primary":         types.StringType,
				"secondary":       types.StringType,
				"ipv6_primary":    types.StringType,
				"ipv6_secondary":  types.StringType,
				"preference":      types.StringType,
				"ipv6_preference": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dnsModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_upnpModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    upnpModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":         types.BoolType,
				"wan_interface":   types.StringType,
				"nat_pmp_enabled": types.BoolType,
				"secure_mode":     types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("upnpModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loadBalanceModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    loadBalanceModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"type":              types.StringType,
				"weight":            types.Int64Type,
				"failover_priority": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadBalanceModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_igmpProxyModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    igmpProxyModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"downstream": types.StringType,
				"upstream":   types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("igmpProxyModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpv6WanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpv6WanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"cos":          types.Int64Type,
				"pd_size":      types.Int64Type,
				"pd_size_auto": types.BoolType,
				"options": types.ListType{
					ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
				},
				"wan_delegation_type": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpv6WanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpWanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpWanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"cos": types.Int64Type,
				"options": types.ListType{
					ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpWanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_wanResource_Metadata(t *testing.T) {
	tests := []struct {
		name             string
		providerTypeName string
		wantTypeName     string
	}{
		{
			name:             "type name includes provider prefix",
			providerTypeName: "unifi",
			wantTypeName:     "unifi_wan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &wanResource{}
			resp := &fwresource.MetadataResponse{}
			r.Metadata(
				context.Background(),
				fwresource.MetadataRequest{ProviderTypeName: tt.providerTypeName},
				resp,
			)
			if resp.TypeName != tt.wantTypeName {
				t.Errorf("Metadata() TypeName = %v, want %v", resp.TypeName, tt.wantTypeName)
			}
		})
	}
}

func Test_wanResource_IdentitySchema(t *testing.T) {
	t.Run("does not panic and returns identity attributes", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.IdentitySchemaResponse{}
		r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("IdentitySchema() returned errors: %v", resp.Diagnostics)
		}
		if len(resp.IdentitySchema.Attributes) == 0 {
			t.Error("IdentitySchema() returned no attributes")
		}
	})
}

func Test_wanResource_Schema(t *testing.T) {
	t.Run("returns schema with key attributes", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.SchemaResponse{}
		r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Schema() returned errors: %v", resp.Diagnostics)
		}
		for _, key := range []string{"id", "name", "type"} {
			if _, ok := resp.Schema.Attributes[key]; !ok {
				t.Errorf("Schema() missing attribute %q", key)
			}
		}
	})
}

func Test_wanResource_Configure(t *testing.T) {
	t.Run("nil provider data is not an error", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.ConfigureResponse{}
		r.Configure(context.Background(), fwresource.ConfigureRequest{ProviderData: nil}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf(
				"Configure() with nil provider data should not error, got: %v",
				resp.Diagnostics,
			)
		}
	})

	t.Run("wrong type produces error", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.ConfigureResponse{}
		r.Configure(context.Background(), fwresource.ConfigureRequest{ProviderData: "wrong"}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("Configure() with wrong type should produce an error")
		}
	})

	t.Run("correct Client type", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.ConfigureResponse{}
		client := &Client{}
		r.Configure(context.Background(), fwresource.ConfigureRequest{ProviderData: client}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("Configure() with *Client should not error, got: %v", resp.Diagnostics)
		}
		if r.client != client {
			t.Error("Configure() did not set client")
		}
	})
}

func Test_wanResource_Create(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

func Test_wanResource_adoptExistingWAN(t *testing.T) {
	t.Skip("requires configured client")
}

func Test_wanResource_overlayConfig(t *testing.T) {
	t.Skip("requires complex state setup")
}

func Test_wanResource_Read(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

func Test_wanResource_Update(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

func Test_wanResource_applyPlanToState(t *testing.T) {
	t.Skip("requires complex state setup")
}

func Test_wanResource_Delete(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

func Test_wanResource_ImportState(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

func Test_wanResource_modelToNetwork(t *testing.T) {
	t.Run("minimal model converts correctly", func(t *testing.T) {
		r := &wanResource{}
		ctx := context.Background()
		model := &wanResourceModel{
			Name:                  types.StringValue("test"),
			Type:                  types.StringValue("dhcp"),
			TypeV6:                types.StringNull(),
			Enabled:               types.BoolValue(true),
			Vlan:                  types.ObjectNull(vlanModel{}.AttributeTypes()),
			EgressQoS:             types.ObjectNull(egressQosModel{}.AttributeTypes()),
			DNS:                   types.ObjectNull(dnsModel{}.AttributeTypes()),
			DHCP:                  types.ObjectNull(dhcpWanModel{}.AttributeTypes()),
			DHCPv6:                types.ObjectNull(dhcpv6WanModel{}.AttributeTypes()),
			SmartQ:                types.ObjectNull(smartqModel{}.AttributeTypes()),
			UPnP:                  types.ObjectNull(upnpModel{}.AttributeTypes()),
			LoadBalance:           types.ObjectNull(loadBalanceModel{}.AttributeTypes()),
			IGMPProxy:             types.ObjectNull(igmpProxyModel{}.AttributeTypes()),
			ProviderCapabilities:  types.ObjectNull(providerCapabilitiesModel{}.AttributeTypes()),
			ReportWANEvent:        types.BoolNull(),
			IPAliases:             types.ListNull(types.StringType),
			SettingPreference:     types.StringNull(),
			IPv6SettingPreference: types.StringNull(),
			SingleNetworkLAN:      types.StringNull(),
			MACOverrideEnabled:    types.BoolNull(),
			DsliteRemoteHost:      types.StringNull(),
			DsliteRemoteHostAuto:  types.BoolNull(),
		}
		got, diags := r.modelToNetwork(ctx, model)
		if diags.HasError() {
			t.Fatalf("modelToNetwork() returned errors: %v", diags)
		}
		if got == nil {
			t.Fatal("modelToNetwork() returned nil network")
		}
		if got.Name == nil || *got.Name != "test" {
			t.Errorf("expected Name=test, got %v", got.Name)
		}
		if got.WANType == nil || *got.WANType != "dhcp" {
			t.Errorf("expected WANType=dhcp, got %v", got.WANType)
		}
		if got.Purpose != "wan" {
			t.Errorf("expected Purpose=wan, got %v", got.Purpose)
		}
		if !got.Enabled {
			t.Error("expected Enabled=true")
		}
	})
}

func Test_wanResource_networkToModel(t *testing.T) {
	t.Run("converts API network back to model", func(t *testing.T) {
		r := &wanResource{}
		ctx := context.Background()
		wanType := "dhcp"
		name := "test-wan"
		network := &unifi.Network{
			ID:      "abc123",
			Name:    &name,
			Purpose: "wan",
			WANType: &wanType,
			Enabled: true,
		}
		model := &wanResourceModel{}
		applyWANDefaults(model)
		diags := r.networkToModel(ctx, network, model, "default")
		if diags.HasError() {
			t.Fatalf("networkToModel() returned errors: %v", diags)
		}
		if model.ID.ValueString() != "abc123" {
			t.Errorf("expected ID=abc123, got %v", model.ID.ValueString())
		}
		if model.Site.ValueString() != "default" {
			t.Errorf("expected Site=default, got %v", model.Site.ValueString())
		}
		if model.Name.ValueString() != "test-wan" {
			t.Errorf("expected Name=test-wan, got %v", model.Name.ValueString())
		}
		if model.Type.ValueString() != "dhcp" {
			t.Errorf("expected Type=dhcp, got %v", model.Type.ValueString())
		}
	})
}

func Test_applyWANDefaults(t *testing.T) {
	t.Run("applies defaults to empty model", func(t *testing.T) {
		model := &wanResourceModel{}
		applyWANDefaults(model)
		if !model.Vlan.IsNull() {
			t.Error("expected Vlan to be null after defaults")
		}
		if !model.EgressQoS.IsNull() {
			t.Error("expected EgressQoS to be null after defaults")
		}
		if !model.SmartQ.IsNull() {
			t.Error("expected SmartQ to be null after defaults")
		}
		if !model.DNS.IsNull() {
			t.Error("expected DNS to be null after defaults")
		}
		if !model.IPAliases.IsNull() {
			t.Error("expected IPAliases to be null after defaults")
		}
	})
}

func Test_wanResource_ListResourceConfigSchema(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwlist.ListResourceSchemaResponse{}
		r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ListResourceConfigSchema() returned errors: %v", resp.Diagnostics)
		}
	})
}

func Test_wanResource_List(t *testing.T) {
	t.Skip("requires configured client")
}

// macOverrideSchema mirrors just the two attributes the mac_override plan
// modifier reads. The real WAN schema has upwards of forty attributes and every
// one of them would have to be spelled out in the raw tftypes value.
func macOverrideSchema() fwschema.Schema {
	return fwschema.Schema{
		Attributes: map[string]fwschema.Attribute{
			"mac_override":         fwschema.StringAttribute{Optional: true},
			"mac_override_enabled": fwschema.BoolAttribute{Optional: true, Computed: true},
		},
	}
}

func macOverridePlanRequest(
	t *testing.T,
	configMAC types.String,
	stateEnabled types.Bool,
	hasState bool,
) planmodifier.BoolRequest {
	t.Helper()
	ctx := context.Background()
	s := macOverrideSchema()
	objType := s.Type().TerraformType(ctx)

	mac := tftypes.NewValue(tftypes.String, nil)
	if !configMAC.IsNull() {
		mac = tftypes.NewValue(tftypes.String, configMAC.ValueString())
	}
	config := tftypes.NewValue(objType, map[string]tftypes.Value{
		"mac_override":         mac,
		"mac_override_enabled": tftypes.NewValue(tftypes.Bool, nil),
	})

	state := tftypes.NewValue(objType, nil)
	if hasState {
		enabled := tftypes.NewValue(tftypes.Bool, nil)
		if !stateEnabled.IsNull() {
			enabled = tftypes.NewValue(tftypes.Bool, stateEnabled.ValueBool())
		}
		state = tftypes.NewValue(objType, map[string]tftypes.Value{
			"mac_override":         mac,
			"mac_override_enabled": enabled,
		})
	}

	return planmodifier.BoolRequest{
		Path:       path.Root("mac_override_enabled"),
		Config:     tfsdk.Config{Schema: s, Raw: config},
		Plan:       tfsdk.Plan{Schema: s, Raw: config},
		State:      tfsdk.State{Schema: s, Raw: state},
		PlanValue:  types.BoolUnknown(),
		StateValue: stateEnabled,
	}
}

// Test_macOverrideEnabledPlanModifier covers the case that motivated the
// modifier: a WAN whose mac_override was deleted from the configuration must not
// keep planning mac_override_enabled = true off prior state, or the apply asks
// the controller to clone an empty address.
func Test_macOverrideEnabledPlanModifier(t *testing.T) {
	ctx := context.Background()
	m := macOverrideEnabledPlanModifier{}

	t.Run("mac removed from config plans false", func(t *testing.T) {
		req := macOverridePlanRequest(t, types.StringNull(), types.BoolValue(true), true)
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		m.PlanModifyBool(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("PlanModifyBool() errored: %v", resp.Diagnostics)
		}
		if resp.PlanValue.IsUnknown() || resp.PlanValue.ValueBool() {
			t.Errorf("expected a planned false, got %v", resp.PlanValue)
		}
	})

	t.Run("mac still configured holds prior state", func(t *testing.T) {
		req := macOverridePlanRequest(t, types.StringValue("02:00:00:00:00:01"), types.BoolValue(true), true)
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		m.PlanModifyBool(ctx, req, resp)
		if !resp.PlanValue.Equal(types.BoolValue(true)) {
			t.Errorf("expected the prior state to be held, got %v", resp.PlanValue)
		}
	})

	t.Run("create with a mac leaves the value unknown", func(t *testing.T) {
		req := macOverridePlanRequest(t, types.StringValue("02:00:00:00:00:01"), types.BoolNull(), false)
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		m.PlanModifyBool(ctx, req, resp)
		if !resp.PlanValue.IsUnknown() {
			t.Errorf("expected the controller to decide, got %v", resp.PlanValue)
		}
	})

	t.Run("enabled without a mac is rejected", func(t *testing.T) {
		req := macOverridePlanRequest(t, types.StringNull(), types.BoolValue(false), true)
		req.PlanValue = types.BoolValue(true)
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		m.PlanModifyBool(ctx, req, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatal("mac_override_enabled = true without mac_override must fail planning")
		}
	})

	t.Run("configured false is left alone", func(t *testing.T) {
		req := macOverridePlanRequest(t, types.StringNull(), types.BoolValue(false), true)
		req.PlanValue = types.BoolValue(false)
		resp := &planmodifier.BoolResponse{PlanValue: req.PlanValue}
		m.PlanModifyBool(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("configured false was rejected: %v", resp.Diagnostics)
		}
		if !resp.PlanValue.Equal(types.BoolValue(false)) {
			t.Errorf("configured false changed to %v", resp.PlanValue)
		}
	})
}

// Test_wanResource_macOverrideRemoval locks the removal path end to end at the
// conversion layer: a null mac_override has to reach the controller as an empty
// string, which is the only value that clears a clone.
func Test_wanResource_macOverrideRemoval(t *testing.T) {
	r := &wanResource{}
	ctx := context.Background()

	newModel := func(mac types.String, enabled types.Bool) *wanResourceModel {
		model := &wanResourceModel{
			Name:               types.StringValue("test"),
			Type:               types.StringValue("dhcp"),
			Enabled:            types.BoolValue(true),
			MACOverride:        mac,
			MACOverrideEnabled: enabled,
		}
		applyWANDefaults(model)
		return model
	}

	t.Run("null mac clears the clone", func(t *testing.T) {
		got, diags := r.modelToNetwork(ctx, newModel(types.StringNull(), types.BoolValue(false)))
		if diags.HasError() {
			t.Fatalf("modelToNetwork() returned errors: %v", diags)
		}
		if got.MACOverride != "" {
			t.Error("expected an empty mac_override to be sent")
		}
		if got.MACOverrideEnabled {
			t.Error("expected the clone flag to follow the address")
		}
	})

	t.Run("stale enabled flag cannot outlive the mac", func(t *testing.T) {
		got, diags := r.modelToNetwork(ctx, newModel(types.StringNull(), types.BoolValue(true)))
		if diags.HasError() {
			t.Fatalf("modelToNetwork() returned errors: %v", diags)
		}
		if got.MACOverrideEnabled {
			t.Error("enabling a clone with no address is not a state the controller can honor")
		}
	})

	t.Run("controller empty value reads back as null", func(t *testing.T) {
		model := &wanResourceModel{}
		applyWANDefaults(model)
		name := "test-wan"
		wanType := "dhcp"
		diags := r.networkToModel(ctx, &unifi.Network{
			ID:      "abc123",
			Name:    &name,
			Purpose: "wan",
			WANType: &wanType,
		}, model, "default")
		if diags.HasError() {
			t.Fatalf("networkToModel() returned errors: %v", diags)
		}
		if !model.MACOverride.IsNull() {
			t.Errorf("expected a null mac_override, got %v", model.MACOverride)
		}
		if model.MACOverrideEnabled.ValueBool() {
			t.Error("expected mac_override_enabled to read back false")
		}
	})
}

// Test_wanResource_Schema_macOverride guards both halves of the mac_override
// contract: it must not be Computed (Optional+Computed retains a removed value
// forever, so the clone could never be cleared through HCL), and its validators
// must not echo the address.
func Test_wanResource_Schema_macOverride(t *testing.T) {
	const sentinel = "de:ad:be:ef:xx:99"
	ctx := context.Background()
	r := &wanResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() returned errors: %v", resp.Diagnostics)
	}

	attr, ok := resp.Schema.Attributes["mac_override"].(fwschema.StringAttribute)
	if !ok {
		t.Fatalf("mac_override is not a StringAttribute: %T", resp.Schema.Attributes["mac_override"])
	}
	if attr.IsComputed() {
		t.Error("mac_override must not be Computed; a removed value would be retained from state")
	}
	if !attr.IsOptional() || !attr.IsSensitive() {
		t.Error("mac_override must stay Optional and Sensitive")
	}
	if len(attr.Validators) == 0 {
		t.Fatal("mac_override lost its validators")
	}

	rejected := false
	for _, v := range attr.Validators {
		vResp := &validator.StringResponse{}
		v.ValidateString(ctx, validator.StringRequest{
			Path:        path.Root("mac_override"),
			ConfigValue: types.StringValue(sentinel),
		}, vResp)
		for _, d := range vResp.Diagnostics {
			if strings.Contains(d.Summary(), sentinel) || strings.Contains(d.Detail(), sentinel) {
				t.Errorf("validator leaks the sensitive value: %s / %s", d.Summary(), d.Detail())
			}
		}
		rejected = rejected || vResp.Diagnostics.HasError()
	}
	if !rejected {
		t.Errorf("no validator rejected %q", sentinel)
	}
}
