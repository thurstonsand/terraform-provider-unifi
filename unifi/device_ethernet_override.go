package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// The controller contract for DeviceEthernetOverrides: `eth[0-9]{1,2}` for the
// interface and `LAN[2-8]?|WAN[2-9]?|MGMT` for the network group.
var (
	ethernetIfnamePattern = regexp.MustCompile(`^eth[0-9]{1,2}$`)

	ethernetNetworkGroups = []string{
		"LAN", "LAN2", "LAN3", "LAN4", "LAN5", "LAN6", "LAN7", "LAN8",
		"WAN", "WAN2", "WAN3", "WAN4", "WAN5", "WAN6", "WAN7", "WAN8", "WAN9",
		"MGMT",
	}
)

// ethernetOverrideModel describes a single ethernet_override block: the
// interface-to-network-group assignment, and nothing else. The controller
// carries more per-interface state (`disabled`, …) which stays unmanaged.
type ethernetOverrideModel struct {
	Ifname       types.String `tfsdk:"ifname"`
	NetworkGroup types.String `tfsdk:"network_group"`
}

func ethernetOverrideAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ifname":        types.StringType,
		"network_group": types.StringType,
	}
}

func ethernetOverrideObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: ethernetOverrideAttrTypes()}
}

// frameworkToEthernetOverrides converts the declared blocks into API structs,
// preserving configuration order. A null, unknown, or empty list yields nil,
// which every downstream step reads as "this field is unmanaged". Empty counts
// as unmanaged because a config with no blocks arrives as an empty list, and
// that is precisely the case where the provider must not touch the field.
func (r *deviceResource) frameworkToEthernetOverrides(
	ctx context.Context,
	list types.List,
) ([]unifi.DeviceEthernetOverrides, diag.Diagnostics) {
	var diags diag.Diagnostics

	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}

	var models []ethernetOverrideModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	if len(models) == 0 {
		return nil, diags
	}

	declared := make([]unifi.DeviceEthernetOverrides, 0, len(models))
	for _, model := range models {
		declared = append(declared, unifi.DeviceEthernetOverrides{
			Ifname:       model.Ifname.ValueString(),
			NetworkGroup: model.NetworkGroup.ValueString(),
		})
	}
	return declared, diags
}

// resolveEthernetOverridesForUpdate builds the ethernet_overrides array to send
// in an update PUT. The UniFi PUT replaces the whole array, so managing a subset
// of interfaces means starting from the controller's current list, in its
// current order, and overwriting only the network group of the declared
// interfaces. Every unmanaged field of every entry — `disabled` above all —
// survives untouched.
//
// A nil `declared` means no ethernet_override block is configured: the field is
// unmanaged, so nil comes back and the caller keeps it off the wire entirely.
func resolveEthernetOverridesForUpdate(
	current, declared []unifi.DeviceEthernetOverrides,
) ([]unifi.DeviceEthernetOverrides, diag.Diagnostics) {
	var diags diag.Diagnostics

	if declared == nil {
		return nil, diags
	}

	liveIndex := make(map[string]int, len(current))
	for i, entry := range current {
		if _, dup := liveIndex[entry.Ifname]; dup {
			diags.AddError(
				"Ambiguous Device Ethernet Overrides",
				fmt.Sprintf(
					"The device reports interface %q more than once in its ethernet_overrides "+
						"list. The provider cannot tell which entry to manage; resolve the "+
						"duplicate on the controller before configuring ethernet_override.",
					entry.Ifname,
				),
			)
			return nil, diags
		}
		liveIndex[entry.Ifname] = i
	}

	merged := make([]unifi.DeviceEthernetOverrides, len(current))
	copy(merged, current)

	seen := make(map[string]struct{}, len(declared))
	for _, entry := range declared {
		if _, dup := seen[entry.Ifname]; dup {
			diags.AddError(
				"Duplicate Ethernet Override",
				fmt.Sprintf(
					"Interface %q is declared in more than one ethernet_override block. "+
						"Declare each interface at most once.",
					entry.Ifname,
				),
			)
			return nil, diags
		}
		seen[entry.Ifname] = struct{}{}

		i, ok := liveIndex[entry.Ifname]
		if !ok {
			diags.AddError(
				"Unknown Ethernet Interface",
				fmt.Sprintf(
					"The device has no interface %q in its ethernet_overrides list. "+
						"Available interfaces: %s.",
					entry.Ifname,
					strings.Join(ethernetIfnames(current), ", "),
				),
			)
			return nil, diags
		}
		merged[i].NetworkGroup = entry.NetworkGroup
	}

	return merged, diags
}

func ethernetIfnames(overrides []unifi.DeviceEthernetOverrides) []string {
	names := make([]string, 0, len(overrides))
	for _, entry := range overrides {
		names = append(names, entry.Ifname)
	}
	return names
}

// refreshEthernetOverrides rebuilds the ethernet_override list from the API
// response, keeping the interfaces the practitioner declared and refreshing
// only their network group, so a controller-side reassignment shows up as
// drift. The controller's other interfaces never enter Terraform state.
func refreshEthernetOverrides(
	ctx context.Context,
	prior types.List,
	live []unifi.DeviceEthernetOverrides,
) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	if prior.IsNull() || prior.IsUnknown() {
		return prior, diags
	}

	var models []ethernetOverrideModel
	diags.Append(prior.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return prior, diags
	}

	liveGroups := make(map[string]string, len(live))
	for _, entry := range live {
		if _, duplicate := liveGroups[entry.Ifname]; duplicate {
			diags.AddError(
				"Ambiguous Device Ethernet Overrides",
				fmt.Sprintf("The device reports interface %q more than once.", entry.Ifname),
			)
			return prior, diags
		}
		liveGroups[entry.Ifname] = entry.NetworkGroup
	}

	for i, model := range models {
		ifname := model.Ifname.ValueString()
		group, ok := liveGroups[ifname]
		if !ok {
			diags.AddError(
				"Missing Device Ethernet Interface",
				fmt.Sprintf(
					"The configured interface %q is absent from the device's ethernet_overrides list.",
					ifname,
				),
			)
			return prior, diags
		}
		models[i].NetworkGroup = types.StringValue(group)
	}

	refreshed, listDiags := types.ListValueFrom(ctx, ethernetOverrideObjectType(), models)
	diags.Append(listDiags...)
	if diags.HasError() {
		return prior, diags
	}
	return refreshed, diags
}
