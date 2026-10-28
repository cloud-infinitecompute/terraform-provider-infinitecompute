// Package provider implements the infinitecompute Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
)

const (
	envAPIKey = "INFINITECOMPUTE_API_KEY"
	envAPIURL = "INFINITECOMPUTE_API_URL"
)

type icProvider struct {
	version string
}

type providerModel struct {
	APIKey types.String `tfsdk:"api_key"`
	APIURL types.String `tfsdk:"api_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &icProvider{version: version} }
}

func (p *icProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "infinitecompute"
	resp.Version = p.version
}

func (p *icProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Infinite Compute GPU servers, volumes and SSH access through the public API.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "An API key from the console (Account → API keys), ic_ck_…. Default: the " + envAPIKey + " environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_url": schema.StringAttribute{
				Description: "The API's base URL. Default: " + envAPIURL + ", else " + api.DefaultURL + ".",
				Optional:    true,
			},
		},
	}
}

func (p *icProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := firstSet(cfg.APIKey, os.Getenv(envAPIKey))
	if key == "" {
		resp.Diagnostics.AddError("No API key",
			"Set api_key in the provider block or the "+envAPIKey+" environment variable. "+
				"Create a key in the console under Account → API keys.")
		return
	}
	c, err := api.New(firstSet(cfg.APIURL, os.Getenv(envAPIURL)), key, "terraform-provider-infinitecompute/"+p.version)
	if err != nil {
		resp.Diagnostics.AddError("Could not create the API client", err.Error())
		return
	}
	resp.DataSourceData = c
	resp.ResourceData = c
}

func firstSet(v types.String, fallback string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return fallback
}

func (p *icProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newServerResource,
		newVolumeResource,
		newVolumeAttachmentResource,
		newSSHKeyResource,
	}
}

func (p *icProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newServerTypeDataSource,
		newImageDataSource,
	}
}
