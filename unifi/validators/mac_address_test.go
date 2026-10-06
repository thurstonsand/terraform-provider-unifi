package validators

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMACAddressValidator(t *testing.T) {
	tests := []struct {
		name string
		want validator.String
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MACAddressValidator(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MACAddressValidator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_macAddressValidator_Description(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		v    macAddressValidator
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.Description(tt.args.ctx); got != tt.want {
				t.Errorf("macAddressValidator.Description() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_macAddressValidator_MarkdownDescription(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		v    macAddressValidator
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.MarkdownDescription(tt.args.ctx); got != tt.want {
				t.Errorf("macAddressValidator.MarkdownDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_macAddressValidator_ValidateString(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  validator.StringRequest
		resp *validator.StringResponse
	}
	tests := []struct {
		name string
		v    macAddressValidator
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.v.ValidateString(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

// TestSensitiveMACAddressValidator proves the validator rejects a malformed
// address without repeating it. Attribute sensitivity does not propagate into
// diagnostics, so any validator that formats the configured value leaks it into
// plan output for every operator watching the run.
func TestSensitiveMACAddressValidator(t *testing.T) {
	const sentinel = "de:ad:be:ef:xx:99"

	t.Run("invalid value never appears in diagnostics", func(t *testing.T) {
		resp := &validator.StringResponse{}
		SensitiveMACAddressValidator().ValidateString(
			context.Background(),
			validator.StringRequest{
				Path:        path.Root("mac_override"),
				ConfigValue: types.StringValue(sentinel),
			},
			resp,
		)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("expected %q to be rejected", sentinel)
		}
		for _, d := range resp.Diagnostics {
			if strings.Contains(d.Summary(), sentinel) || strings.Contains(d.Detail(), sentinel) {
				t.Errorf("diagnostic leaks the sensitive value: %s / %s", d.Summary(), d.Detail())
			}
		}
	})

	t.Run("valid, null and unknown values pass", func(t *testing.T) {
		for _, v := range []types.String{
			types.StringValue("02:00:00:00:00:01"),
			types.StringNull(),
			types.StringUnknown(),
		} {
			resp := &validator.StringResponse{}
			SensitiveMACAddressValidator().ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root("mac_override"), ConfigValue: v},
				resp,
			)
			if resp.Diagnostics.HasError() {
				t.Errorf("value %v was rejected: %v", v, resp.Diagnostics)
			}
		}
	})

	t.Run("hyphenated MAC is rejected", func(t *testing.T) {
		resp := &validator.StringResponse{}
		SensitiveMACAddressValidator().ValidateString(
			context.Background(),
			validator.StringRequest{
				Path:        path.Root("mac_override"),
				ConfigValue: types.StringValue("02-00-00-00-00-01"),
			},
			resp,
		)
		if !resp.Diagnostics.HasError() {
			t.Error("the controller only accepts colon-delimited MACs")
		}
	})
}
