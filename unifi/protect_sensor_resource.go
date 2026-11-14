package unifi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/protect"
)

var (
	_ resource.Resource                = &protectSensorResource{}
	_ resource.ResourceWithConfigure   = &protectSensorResource{}
	_ resource.ResourceWithImportState = &protectSensorResource{}
	_ resource.ResourceWithIdentity    = &protectSensorResource{}
)

type protectSensorResource struct {
	client *Client
}

type protectSensorResourceModel struct {
	ID                         types.String       `tfsdk:"id"`
	MAC                        hwtypes.MACAddress `tfsdk:"mac"`
	Type                       types.String       `tfsdk:"type"`
	Name                       types.String       `tfsdk:"name"`
	MotionEnabled              types.Bool         `tfsdk:"motion_enabled"`
	MotionSensitivity          types.Int64        `tfsdk:"motion_sensitivity"`
	MotionSensitivityWhenArmed types.Int64        `tfsdk:"motion_sensitivity_when_armed"`
	CustomSensitivityWhenArmed types.Bool         `tfsdk:"custom_sensitivity_when_armed"`
	ScheduleMode               types.String       `tfsdk:"schedule_mode"`
}

// protectSensorPatch is the PATCH body this resource sends. It has no
// alarmSettings member, so the provider cannot change smoke/CO alarm
// configuration; the generated request body type would allow it.
type protectSensorPatch struct {
	Name                          *string                     `json:"name,omitempty"`
	MotionSettings                *protect.MotionSettings     `json:"motionSettings,omitempty"`
	ScheduleMode                  *protect.SensorScheduleMode `json:"scheduleMode,omitempty"`
	HasCustomSensitivityWhenArmed *bool                       `json:"hasCustomSensitivityWhenArmed,omitempty"`
}

func NewProtectSensorResource() resource.Resource {
	return &protectSensorResource{}
}

func (r *protectSensorResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_protect_sensor"
}

func (r *protectSensorResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "`unifi_protect_sensor` manages selected settings of a UniFi Protect sensor that is " +
			"already adopted in Protect. Adoption stays manual: creating the resource takes over the adopted " +
			"sensor with the given MAC, and destroying it only removes it from Terraform state, leaving the " +
			"sensor adopted and configured as it was. Only the settings set in configuration are managed; " +
			"removing one from configuration stops managing it without changing it. Smoke and carbon monoxide " +
			"alarm settings are not managed.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The Protect ID of the sensor. It changes when the sensor is re-adopted.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"mac": schema.StringAttribute{
				MarkdownDescription: "The MAC address of the adopted sensor.",
				CustomType:          hwtypes.MACAddressType{},
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The sensor model, such as `USL-Motion-US`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The sensor name.",
				Optional:            true,
			},
			"motion_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether motion detection is enabled.",
				Optional:            true,
			},
			"motion_sensitivity": schema.Int64Attribute{
				MarkdownDescription: "Motion sensitivity from 0 to 100, used unless the system is armed with " +
					"`custom_sensitivity_when_armed` enabled.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, 100)},
			},
			"motion_sensitivity_when_armed": schema.Int64Attribute{
				MarkdownDescription: "Motion sensitivity from 0 to 100 while the system is armed and " +
					"`custom_sensitivity_when_armed` is enabled.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(0, 100)},
			},
			"custom_sensitivity_when_armed": schema.BoolAttribute{
				MarkdownDescription: "Whether `motion_sensitivity_when_armed` applies while the system is armed.",
				Optional:            true,
			},
			"schedule_mode": schema.StringAttribute{
				MarkdownDescription: "When detection runs: `always`, or `when_armed`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(protect.SensorScheduleModeAlways),
						string(protect.SensorScheduleModeWhenArmed),
					),
				},
			},
		},
	}
}

func (r *protectSensorResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"mac": identityschema.StringAttribute{
				CustomType:        hwtypes.MACAddressType{},
				RequiredForImport: true,
			},
		},
	}
}

func (r *protectSensorResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}
	r.client = client
}

func (r *protectSensorResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan protectSensorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sensor, err := r.findSensorByMAC(ctx, plan.MAC.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Protect Sensors", err.Error())
		return
	}
	if sensor == nil {
		resp.Diagnostics.AddError(
			"Protect Sensor Not Adopted",
			fmt.Sprintf(
				"No sensor with MAC %s is adopted in UniFi Protect. Adopt it in Protect first.",
				plan.MAC.ValueString(),
			),
		)
		return
	}

	sensor, err = r.patchSensor(ctx, sensor, sensorPatch(plan, protectSensorAttributes(sensor)))
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Protect Sensor", err.Error())
		return
	}

	r.setState(ctx, &resp.State, resp.Identity, plan, sensor, &resp.Diagnostics)
}

func (r *protectSensorResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state protectSensorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var sensor *protect.Sensor
	var err error
	if id := state.ID.ValueString(); id != "" {
		sensor, err = r.getSensor(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error Reading Protect Sensor", err.Error())
			return
		}
	}
	if sensor == nil {
		// Re-adoption gives the sensor a new ID but keeps its MAC; import
		// supplies only the MAC.
		sensor, err = r.findSensorByMAC(ctx, state.MAC.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error Reading Protect Sensors", err.Error())
			return
		}
	}
	if sensor == nil {
		resp.Diagnostics.AddWarning(
			"Protect Sensor No Longer Adopted",
			fmt.Sprintf(
				"No sensor with MAC %s is adopted in UniFi Protect, so it was removed from state.",
				state.MAC.ValueString(),
			),
		)
		resp.State.RemoveResource(ctx)
		return
	}

	r.setState(ctx, &resp.State, resp.Identity, state, sensor, &resp.Diagnostics)
}

func (r *protectSensorResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, state protectSensorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sensor, err := r.getSensor(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Protect Sensor", err.Error())
		return
	}
	if sensor == nil {
		resp.Diagnostics.AddError(
			"Protect Sensor Not Found",
			fmt.Sprintf("Protect sensor %s no longer exists; refresh to find it by MAC.", state.ID.ValueString()),
		)
		return
	}

	sensor, err = r.patchSensor(ctx, sensor, sensorPatch(plan, protectSensorAttributes(sensor)))
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Protect Sensor", err.Error())
		return
	}

	r.setState(ctx, &resp.State, resp.Identity, plan, sensor, &resp.Diagnostics)
}

// Delete leaves the sensor in Protect untouched. Unadopting a sensor removes
// it from the system, and for a smoke or CO detector that is a safety change
// no Terraform declaration should be able to make.
func (r *protectSensorResource) Delete(
	_ context.Context,
	_ resource.DeleteRequest,
	_ *resource.DeleteResponse,
) {
}

func (r *protectSensorResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	mac := hwtypes.NewMACAddressValue(req.ID)
	if req.ID == "" {
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("mac"), &mac)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("mac"), mac)...)
}

// setState records sensor as the resource's state. Settings the model leaves
// null stay null, so settings nobody manages never show as drift.
func (r *protectSensorResource) setState(
	ctx context.Context,
	state interface {
		Set(context.Context, any) diag.Diagnostics
	},
	identity *tfsdk.ResourceIdentity,
	managed protectSensorResourceModel,
	sensor *protect.Sensor,
	diags *diag.Diagnostics,
) {
	remote := protectSensorAttributes(sensor)
	model := protectSensorResourceModel{
		ID:                         types.StringValue(sensor.Id),
		MAC:                        managed.MAC,
		Type:                       remote.Type,
		Name:                       keepManaged(managed.Name, remote.Name),
		MotionEnabled:              keepManaged(managed.MotionEnabled, remote.MotionEnabled),
		MotionSensitivity:          keepManaged(managed.MotionSensitivity, remote.MotionSensitivity),
		MotionSensitivityWhenArmed: keepManaged(managed.MotionSensitivityWhenArmed, remote.MotionSensitivityWhenArmed),
		CustomSensitivityWhenArmed: keepManaged(managed.CustomSensitivityWhenArmed, remote.CustomSensitivityWhenArmed),
		ScheduleMode:               keepManaged(managed.ScheduleMode, remote.ScheduleMode),
	}
	diags.Append(state.Set(ctx, model)...)
	if identity != nil {
		diags.Append(identity.SetAttribute(ctx, path.Root("mac"), managed.MAC)...)
	}
}

func keepManaged[T interface{ IsNull() bool }](managed, remote T) T {
	if managed.IsNull() {
		return managed
	}
	return remote
}

// protectSensorAttributes reads every setting this resource can manage from
// sensor.
func protectSensorAttributes(sensor *protect.Sensor) protectSensorResourceModel {
	sensitivity := func(v *float32) types.Int64 {
		if v == nil {
			return types.Int64Null()
		}
		return types.Int64Value(int64(math.Round(float64(*v))))
	}
	return protectSensorResourceModel{
		Type:                       types.StringPointerValue(sensor.Type),
		Name:                       types.StringPointerValue(sensor.Name),
		MotionEnabled:              types.BoolPointerValue(sensor.MotionSettings.IsEnabled),
		MotionSensitivity:          sensitivity(sensor.MotionSettings.Sensitivity),
		MotionSensitivityWhenArmed: sensitivity(sensor.MotionSettings.SensitivityWhenArmed),
		CustomSensitivityWhenArmed: types.BoolValue(sensor.HasCustomSensitivityWhenArmed),
		ScheduleMode:               types.StringValue(string(sensor.ScheduleMode)),
	}
}

// sensorPatch holds each setting the plan manages whose value differs from
// current. Unmanaged settings and settings already at their planned value are
// left out, so the console merges only real changes.
func sensorPatch(plan, current protectSensorResourceModel) protectSensorPatch {
	changed := func(planned, actual attr.Value) bool {
		return !planned.IsNull() && !planned.Equal(actual)
	}
	sensitivity := func(v types.Int64) *float32 {
		f := float32(v.ValueInt64())
		return &f
	}

	var patch protectSensorPatch
	if changed(plan.Name, current.Name) {
		patch.Name = plan.Name.ValueStringPointer()
	}
	var motion protect.MotionSettings
	if changed(plan.MotionEnabled, current.MotionEnabled) {
		motion.IsEnabled = plan.MotionEnabled.ValueBoolPointer()
	}
	if changed(plan.MotionSensitivity, current.MotionSensitivity) {
		motion.Sensitivity = sensitivity(plan.MotionSensitivity)
	}
	if changed(plan.MotionSensitivityWhenArmed, current.MotionSensitivityWhenArmed) {
		motion.SensitivityWhenArmed = sensitivity(plan.MotionSensitivityWhenArmed)
	}
	if motion != (protect.MotionSettings{}) {
		patch.MotionSettings = &motion
	}
	if changed(plan.CustomSensitivityWhenArmed, current.CustomSensitivityWhenArmed) {
		patch.HasCustomSensitivityWhenArmed = plan.CustomSensitivityWhenArmed.ValueBoolPointer()
	}
	if changed(plan.ScheduleMode, current.ScheduleMode) {
		mode := protect.SensorScheduleMode(plan.ScheduleMode.ValueString())
		patch.ScheduleMode = &mode
	}
	return patch
}

// patchSensor sends patch and returns the updated sensor, or sensor itself
// when patch changes nothing.
func (r *protectSensorResource) patchSensor(
	ctx context.Context,
	sensor *protect.Sensor,
	patch protectSensorPatch,
) (*protect.Sensor, error) {
	if patch == (protectSensorPatch{}) {
		return sensor, nil
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Protect.PatchV1SensorsIdWithBodyWithResponse(
		ctx, sensor.Id, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, protectResponseError("PATCH", "sensors/"+sensor.Id, resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

// getSensor returns the sensor with id, or nil when Protect has none.
func (r *protectSensorResource) getSensor(ctx context.Context, id string) (*protect.Sensor, error) {
	resp, err := r.client.Protect.GetV1SensorsIdWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.JSON200 == nil {
		return nil, protectResponseError("GET", "sensors/"+id, resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

// findSensorByMAC returns the adopted sensor with mac, or nil when Protect has
// none. Protect reports MACs as bare uppercase hex.
func (r *protectSensorResource) findSensorByMAC(ctx context.Context, mac string) (*protect.Sensor, error) {
	resp, err := r.client.Protect.GetV1SensorsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, protectResponseError("GET", "sensors", resp.StatusCode(), resp.Body)
	}
	want := protectMAC(mac)
	for i := range *resp.JSON200 {
		if protectMAC((*resp.JSON200)[i].Mac) == want {
			return &(*resp.JSON200)[i], nil
		}
	}
	return nil, nil
}

func protectMAC(mac string) string {
	return strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac))
}

func protectResponseError(method, path string, status int, body []byte) error {
	err := fmt.Errorf("protect %s %s: HTTP %d: %s", method, path, status, strings.TrimSpace(string(body)))
	if status == http.StatusUnauthorized {
		return fmt.Errorf("%w (the Protect Integration API may require the provider's api_key instead of username and password)", err)
	}
	return err
}
