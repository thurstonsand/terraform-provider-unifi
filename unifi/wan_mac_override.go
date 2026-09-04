package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ planmodifier.Bool = macOverrideEnabledPlanModifier{}

// macOverrideEnabledPlanModifier keeps mac_override_enabled tied to
// mac_override. The flag is Optional+Computed, so an unconfigured value plans as
// unknown; plain UseStateForUnknown would resolve it to the prior true even
// after the MAC itself was deleted from the configuration, and the apply would
// then ask the controller to clone an empty address. When the configuration
// carries no MAC, the flag plans false; otherwise it falls back to the prior
// state so the controller keeps whatever it computed.
type macOverrideEnabledPlanModifier struct{}

func (m macOverrideEnabledPlanModifier) Description(ctx context.Context) string {
	return "planned false when mac_override is absent from the configuration, otherwise held at the prior state"
}

func (m macOverrideEnabledPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m macOverrideEnabledPlanModifier) PlanModifyBool(
	ctx context.Context,
	req planmodifier.BoolRequest,
	resp *planmodifier.BoolResponse,
) {
	// Destroy plans carry a null plan; leave them alone.
	if req.Plan.Raw.IsNull() {
		return
	}
	var configMAC types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mac_override"), &configMAC)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !req.PlanValue.IsUnknown() {
		if req.PlanValue.ValueBool() && configMAC.IsNull() {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Missing MAC Override",
				"mac_override_enabled cannot be true unless mac_override is configured.",
			)
		}
		return
	}

	if configMAC.IsNull() {
		resp.PlanValue = types.BoolValue(false)
		return
	}

	if req.State.Raw.IsNull() || req.StateValue.IsNull() {
		return
	}
	resp.PlanValue = req.StateValue
}
