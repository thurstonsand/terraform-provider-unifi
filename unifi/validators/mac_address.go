package validators

import (
	"context"
	"fmt"
	"net"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// MACAddressValidator validates that a string is a valid MAC address.
func MACAddressValidator() validator.String {
	return &macAddressValidator{}
}

type macAddressValidator struct{}

func (v macAddressValidator) Description(ctx context.Context) string {
	return "value must be a valid MAC address"
}

func (v macAddressValidator) MarkdownDescription(ctx context.Context) string {
	return "value must be a valid MAC address"
}

func (v macAddressValidator) ValidateString(
	ctx context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	value := req.ConfigValue.ValueString()
	_, err := net.ParseMAC(value)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid MAC Address",
			fmt.Sprintf("Value %q is not a valid MAC address: %s", value, err.Error()),
		)
	}
}

// SensitiveMACAddressValidator validates a colon-delimited MAC address without
// ever repeating the value back. Attribute sensitivity does not reach
// diagnostics, so the stock regex and MAC validators both print the offending
// address into plan output and logs; use this one wherever the MAC itself is
// the secret.
func SensitiveMACAddressValidator() validator.String {
	return &sensitiveMACAddressValidator{}
}

type sensitiveMACAddressValidator struct{}

func (v sensitiveMACAddressValidator) Description(ctx context.Context) string {
	return "value must be a colon-delimited MAC address"
}

func (v sensitiveMACAddressValidator) MarkdownDescription(ctx context.Context) string {
	return "value must be a colon-delimited MAC address"
}

var colonDelimitedMAC = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

func (v sensitiveMACAddressValidator) ValidateString(
	ctx context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	if !colonDelimitedMAC.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid MAC Address",
			"The configured value is not a colon-delimited MAC address "+
				"(for example 02:00:00:00:00:01). The value is sensitive and is "+
				"therefore not repeated here.",
		)
	}
}
