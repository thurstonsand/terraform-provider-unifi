package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/protect"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

const protectTestMAC = "24:a4:3c:3d:fe:b9"

// fakeProtect is a UniFi OS console with one adopted Protect sensor. It
// records every Protect request other than GETs and applies PATCHes the way
// Protect merges them.
type fakeProtect struct {
	sensor map[string]any
	writes []string
}

func newFakeProtect(t *testing.T, sensorJSON string) (*fakeProtect, *Client) {
	t.Helper()
	f := &fakeProtect{}
	if err := json.Unmarshal([]byte(sensorJSON), &f.sensor); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
			return
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			w.Header().Set("X-Csrf-Token", "csrf")
			w.WriteHeader(http.StatusOK)
			return
		case r.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.0.0"}}`))
			return
		}

		const sensors = "/proxy/protect/integration/v1/sensors"
		id, _ := f.sensor["id"].(string)
		if r.Method != http.MethodGet {
			body, _ := io.ReadAll(r.Body)
			f.writes = append(f.writes, r.Method+" "+r.URL.Path+" "+string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == sensors:
			_ = json.NewEncoder(w).Encode([]any{f.sensor})
		case r.Method == http.MethodGet && r.URL.Path == sensors+"/"+id:
			_ = json.NewEncoder(w).Encode(f.sensor)
		case r.Method == http.MethodPatch && r.URL.Path == sensors+"/"+id:
			var patch map[string]any
			_ = json.Unmarshal([]byte(strings.SplitN(f.writes[len(f.writes)-1], " ", 3)[2]), &patch)
			for k, v := range patch {
				nested, isObject := v.(map[string]any)
				current, hasObject := f.sensor[k].(map[string]any)
				if isObject && hasObject {
					for nk, nv := range nested {
						current[nk] = nv
					}
					continue
				}
				f.sensor[k] = v
			}
			_ = json.NewEncoder(w).Encode(f.sensor)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found","name":"NOT_FOUND"}`))
		}
	}))
	t.Cleanup(srv.Close)

	apiClient, err := unifi.New(context.Background(), &unifi.Config{
		BaseURL:  srv.URL,
		Username: "admin",
		Password: "admin",
	})
	if err != nil {
		t.Fatalf("creating client against fake console: %v", err)
	}
	protectClient, err := protect.New(apiClient)
	if err != nil {
		t.Fatal(err)
	}
	return f, &Client{ApiClient: apiClient, Site: "default", Protect: protectClient}
}

const motionSensorJSON = `{
	"id": "66d025b301ebc903e80003ea",
	"mac": "24A43C3DFEB9",
	"modelKey": "sensor",
	"type": "USL-Motion-US",
	"name": "Main Closet Motion Sensor",
	"state": "CONNECTED",
	"motionSettings": {"isEnabled": true, "sensitivity": 40, "sensitivityWhenArmed": 80},
	"alarmSettings": {"isEnabled": false},
	"scheduleMode": "always",
	"hasCustomSensitivityWhenArmed": false
}`

func protectSensorSchema(t *testing.T) fwresource.SchemaResponse {
	t.Helper()
	resp := fwresource.SchemaResponse{}
	(&protectSensorResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	return resp
}

// configModel is a configuration that sets only the given MAC; callers set the
// attributes they manage.
func configModel() protectSensorResourceModel {
	return protectSensorResourceModel{
		ID:                         types.StringUnknown(),
		MAC:                        hwtypes.NewMACAddressValue(protectTestMAC),
		Type:                       types.StringUnknown(),
		Name:                       types.StringNull(),
		MotionEnabled:              types.BoolNull(),
		MotionSensitivity:          types.Int64Null(),
		MotionSensitivityWhenArmed: types.Int64Null(),
		CustomSensitivityWhenArmed: types.BoolNull(),
		ScheduleMode:               types.StringNull(),
	}
}

func createSensor(
	t *testing.T,
	client *Client,
	model protectSensorResourceModel,
) (tfsdk.State, protectSensorResourceModel, *fwresource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	s := protectSensorSchema(t).Schema
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	if d := plan.Set(ctx, model); d.HasError() {
		t.Fatalf("plan.Set: %v", d)
	}
	resp := &fwresource.CreateResponse{
		State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)},
	}
	(&protectSensorResource{client: client}).Create(ctx, fwresource.CreateRequest{Plan: plan}, resp)

	var got protectSensorResourceModel
	if !resp.Diagnostics.HasError() {
		if d := resp.State.Get(ctx, &got); d.HasError() {
			t.Fatalf("state.Get: %v", d)
		}
	}
	return resp.State, got, resp
}

func readSensor(t *testing.T, client *Client, state tfsdk.State) (protectSensorResourceModel, *fwresource.ReadResponse) {
	t.Helper()
	ctx := context.Background()
	resp := &fwresource.ReadResponse{State: state}
	(&protectSensorResource{client: client}).Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", resp.Diagnostics)
	}
	var got protectSensorResourceModel
	if !resp.State.Raw.IsNull() {
		if d := resp.State.Get(ctx, &got); d.HasError() {
			t.Fatalf("state.Get: %v", d)
		}
	}
	return got, resp
}

// TestProtectSensorCreateSendsOnlyChangedManagedSettings proves create takes
// over the adopted sensor and patches exactly the declared settings that
// differ: the matching name and every unmanaged setting stay out of the body,
// and the merged motion settings keep isEnabled.
func TestProtectSensorCreateSendsOnlyChangedManagedSettings(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)

	model := configModel()
	model.Name = types.StringValue("Main Closet Motion Sensor")
	model.MotionSensitivity = types.Int64Value(60)
	_, got, resp := createSensor(t, client, model)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}

	want := []string{`PATCH /proxy/protect/integration/v1/sensors/66d025b301ebc903e80003ea {"motionSettings":{"sensitivity":60}}`}
	if strings.Join(fake.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes = %q, want %q", fake.writes, want)
	}
	if got.ID.ValueString() != "66d025b301ebc903e80003ea" || got.Type.ValueString() != "USL-Motion-US" {
		t.Errorf("id, type = %s, %s", got.ID, got.Type)
	}
	if got.MotionSensitivity.ValueInt64() != 60 || got.Name.ValueString() != "Main Closet Motion Sensor" {
		t.Errorf("managed state = %s, %s", got.Name, got.MotionSensitivity)
	}
	if !got.MotionEnabled.IsNull() || !got.ScheduleMode.IsNull() || !got.MotionSensitivityWhenArmed.IsNull() {
		t.Errorf("unmanaged settings entered state: %+v", got)
	}
	if enabled := fake.sensor["motionSettings"].(map[string]any)["isEnabled"]; enabled != true {
		t.Errorf("motion isEnabled after patch = %v, want true", enabled)
	}
}

// TestProtectSensorCreateMatchingSendsNothing proves taking over a sensor that
// already matches its declaration writes nothing.
func TestProtectSensorCreateMatchingSendsNothing(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)

	model := configModel()
	model.Name = types.StringValue("Main Closet Motion Sensor")
	model.MotionEnabled = types.BoolValue(true)
	model.MotionSensitivity = types.Int64Value(40)
	if _, _, resp := createSensor(t, client, model); resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("writes = %q, want none", fake.writes)
	}
}

// TestProtectSensorNeverWritesAlarmOrUnadopts pins the safety contract: a
// change to every writable setting never sends alarmSettings, and destroying
// the resource sends nothing at all.
func TestProtectSensorNeverWritesAlarmOrUnadopts(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)

	model := configModel()
	model.Name = types.StringValue("Renamed")
	model.MotionEnabled = types.BoolValue(false)
	model.MotionSensitivity = types.Int64Value(0)
	model.MotionSensitivityWhenArmed = types.Int64Value(10)
	model.CustomSensitivityWhenArmed = types.BoolValue(true)
	model.ScheduleMode = types.StringValue("when_armed")
	state, _, resp := createSensor(t, client, model)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}
	want := `PATCH /proxy/protect/integration/v1/sensors/66d025b301ebc903e80003ea ` +
		`{"name":"Renamed","motionSettings":{"isEnabled":false,"sensitivity":0,"sensitivityWhenArmed":10},` +
		`"scheduleMode":"when_armed","hasCustomSensitivityWhenArmed":true}`
	if len(fake.writes) != 1 || fake.writes[0] != want {
		t.Fatalf("writes = %q, want [%q]", fake.writes, want)
	}

	fake.writes = nil
	deleteResp := &fwresource.DeleteResponse{State: state}
	(&protectSensorResource{client: client}).Delete(
		context.Background(), fwresource.DeleteRequest{State: state}, deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Fatalf("delete diagnostics: %v", deleteResp.Diagnostics)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("destroy wrote %q, want nothing", fake.writes)
	}
}

// TestProtectSensorUpdateStopsManagingRemovedSettings proves removing a
// setting from configuration leaves it as it is in Protect and out of state,
// while a still-managed setting that changed is patched.
func TestProtectSensorUpdateStopsManagingRemovedSettings(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)
	ctx := context.Background()

	model := configModel()
	model.Name = types.StringValue("Main Closet Motion Sensor")
	model.MotionSensitivity = types.Int64Value(40)
	state, created, resp := createSensor(t, client, model)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}

	planned := created
	planned.Name = types.StringNull()
	planned.MotionSensitivity = types.Int64Value(70)
	s := protectSensorSchema(t).Schema
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	if d := plan.Set(ctx, planned); d.HasError() {
		t.Fatalf("plan.Set: %v", d)
	}
	updateResp := &fwresource.UpdateResponse{State: state}
	(&protectSensorResource{client: client}).Update(ctx, fwresource.UpdateRequest{Plan: plan, State: state}, updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", updateResp.Diagnostics)
	}

	want := `PATCH /proxy/protect/integration/v1/sensors/66d025b301ebc903e80003ea {"motionSettings":{"sensitivity":70}}`
	if len(fake.writes) != 1 || fake.writes[0] != want {
		t.Fatalf("writes = %q, want [%q]", fake.writes, want)
	}
	var got protectSensorResourceModel
	if d := updateResp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state.Get: %v", d)
	}
	if !got.Name.IsNull() || got.MotionSensitivity.ValueInt64() != 70 {
		t.Errorf("state name, sensitivity = %s, %s; want null, 70", got.Name, got.MotionSensitivity)
	}
	if fake.sensor["name"] != "Main Closet Motion Sensor" {
		t.Errorf("Protect name = %v, want it unchanged", fake.sensor["name"])
	}
}

// TestProtectSensorReadFollowsReadoption proves a sensor re-adopted under a
// new ID is found again by MAC, and that drift in a managed setting shows
// while drift in an unmanaged one does not.
func TestProtectSensorReadFollowsReadoption(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)

	model := configModel()
	model.Name = types.StringValue("Main Closet Motion Sensor")
	state, _, resp := createSensor(t, client, model)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}

	fake.sensor["id"] = "77e136c412fcd014f91114fb"
	fake.sensor["name"] = "Renamed in the UI"
	fake.sensor["scheduleMode"] = "when_armed"

	got, _ := readSensor(t, client, state)
	if got.ID.ValueString() != "77e136c412fcd014f91114fb" {
		t.Errorf("id = %s, want the re-adopted id", got.ID)
	}
	if got.Name.ValueString() != "Renamed in the UI" {
		t.Errorf("name = %s, want the drifted name", got.Name)
	}
	if !got.ScheduleMode.IsNull() {
		t.Errorf("unmanaged schedule_mode = %s, want null", got.ScheduleMode)
	}
}

// TestProtectSensorUnadopted proves a declaration for a sensor Protect does not
// have fails create, and that one which disappears leaves state on refresh.
func TestProtectSensorUnadopted(t *testing.T) {
	fake, client := newFakeProtect(t, motionSensorJSON)

	model := configModel()
	model.MAC = hwtypes.NewMACAddressValue("00:11:22:33:44:55")
	_, _, resp := createSensor(t, client, model)
	if !resp.Diagnostics.HasError() || resp.Diagnostics[0].Summary() != "Protect Sensor Not Adopted" {
		t.Fatalf("create diagnostics = %v, want Protect Sensor Not Adopted", resp.Diagnostics)
	}

	state, _, resp := createSensor(t, client, configModel())
	if resp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", resp.Diagnostics)
	}
	fake.sensor["id"] = "77e136c412fcd014f91114fb"
	fake.sensor["mac"] = "001122334455"
	_, readResp := readSensor(t, client, state)
	if !readResp.State.Raw.IsNull() {
		t.Fatal("state kept a sensor Protect no longer has")
	}
}
