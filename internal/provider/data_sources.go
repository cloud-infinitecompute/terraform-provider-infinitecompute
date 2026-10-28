package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/client"
)

// --- infinitecompute_server_type -----------------------------------------------------

type serverTypeDataSource struct{ c *api.Client }

type serverTypeModel struct {
	Name     types.String  `tfsdk:"name"`
	GPUModel types.String  `tfsdk:"gpu_model"`
	Site     types.String  `tfsdk:"site"`
	ID       types.String  `tfsdk:"id"`
	VCPUs    types.Int64   `tfsdk:"vcpus"`
	RAMGB    types.Float64 `tfsdk:"ram_gb"`
	GPUName  types.String  `tfsdk:"gpu_name"`
	GPUCount types.Int64   `tfsdk:"gpu_count"`
	Price    types.String  `tfsdk:"price_per_hour"`
	Currency types.String  `tfsdk:"currency"`
}

func newServerTypeDataSource() datasource.DataSource { return &serverTypeDataSource{} }

func (d *serverTypeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_type"
}

func (d *serverTypeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One server type this project can launch, found by name or by GPU model and site. Exactly one must match.",
		Attributes: map[string]schema.Attribute{
			"name":           schema.StringAttribute{Optional: true, Computed: true},
			"gpu_model":      schema.StringAttribute{Optional: true, Computed: true, Description: `GPU model id, e.g. h100 or rtx6000a; "none" for CPU-only types.`},
			"site":           schema.StringAttribute{Optional: true, Computed: true, Description: "Site id, as a server type reports it."},
			"id":             schema.StringAttribute{Computed: true},
			"vcpus":          schema.Int64Attribute{Computed: true},
			"ram_gb":         schema.Float64Attribute{Computed: true},
			"gpu_name":       schema.StringAttribute{Computed: true},
			"gpu_count":      schema.Int64Attribute{Computed: true},
			"price_per_hour": schema.StringAttribute{Computed: true, Description: "Decimal string; empty when not priced."},
			"currency":       schema.StringAttribute{Computed: true},
		},
	}
}

func (d *serverTypeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func gpuModelOf(t client.ServerType) string {
	if t.Gpu == nil {
		return "none"
	}
	return t.Gpu.Model
}

func matchServerTypes(all []client.ServerType, name, gpu, site string) []client.ServerType {
	var out []client.ServerType
	for _, t := range all {
		if (name == "" || t.Name == name) && (gpu == "" || gpuModelOf(t) == gpu) && (site == "" || t.Site.Id == site) {
			out = append(out, t)
		}
	}
	return out
}

func (d *serverTypeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m serverTypeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.c.ServerTypes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Could not list server types", err.Error())
		return
	}
	found := matchServerTypes(all, m.Name.ValueString(), m.GPUModel.ValueString(), m.Site.ValueString())
	if len(found) != 1 {
		names := make([]string, 0, len(found))
		for _, t := range found {
			names = append(names, t.Name)
		}
		resp.Diagnostics.AddError("No single server type matches",
			fmt.Sprintf("%d match (%s). Narrow it with name, gpu_model or site.", len(found), strings.Join(names, ", ")))
		return
	}
	t := found[0]
	m.ID, m.Name, m.Site = types.StringValue(t.Id), types.StringValue(t.Name), types.StringValue(t.Site.Id)
	m.GPUModel = types.StringValue(gpuModelOf(t))
	m.VCPUs, m.RAMGB = types.Int64Value(int64(t.Vcpus)), types.Float64Value(float64(t.RamGb))
	m.GPUName, m.GPUCount = types.StringValue(""), types.Int64Value(0)
	if t.Gpu != nil {
		m.GPUName, m.GPUCount = types.StringValue(t.Gpu.Name), types.Int64Value(int64(t.Gpu.Count))
	}
	m.Price, m.Currency = types.StringValue(""), types.StringValue("")
	if t.Price != nil {
		m.Price, m.Currency = types.StringValue(t.Price.Amount), types.StringValue(t.Price.Currency)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// --- infinitecompute_image -----------------------------------------------------------

type imageDataSource struct{ c *api.Client }

type imageModel struct {
	Name      types.String `tfsdk:"name"`
	GPUModel  types.String `tfsdk:"gpu_model"`
	ID        types.String `tfsdk:"id"`
	GPUModels types.List   `tfsdk:"gpu_models"`
	LoginUser types.String `tfsdk:"login_user"`
	MinDiskGB types.Int64  `tfsdk:"min_disk_gb"`
}

func newImageDataSource() datasource.DataSource { return &imageDataSource{} }

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One image this project can launch from, by name and/or the GPU it's built for. Exactly one must match.",
		Attributes: map[string]schema.Attribute{
			"name":        schema.StringAttribute{Optional: true, Computed: true},
			"gpu_model":   schema.StringAttribute{Optional: true, Description: `Only images built for this GPU model (a server type's gpu_model; "none" for CPU-only).`},
			"id":          schema.StringAttribute{Computed: true},
			"gpu_models":  schema.ListAttribute{Computed: true, ElementType: types.StringType},
			"login_user":  schema.StringAttribute{Computed: true},
			"min_disk_gb": schema.Int64Attribute{Computed: true},
		},
	}
}

func (d *imageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func matchImages(all []client.Image, name, gpu string) []client.Image {
	var out []client.Image
	for _, i := range all {
		if i.Status != "active" {
			continue
		}
		if (name == "" || i.Name == name) && (gpu == "" || slices.Contains(i.GpuModels, gpu)) {
			out = append(out, i)
		}
	}
	return out
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m imageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.c.Images(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Could not list images", err.Error())
		return
	}
	found := matchImages(all, m.Name.ValueString(), m.GPUModel.ValueString())
	if len(found) != 1 {
		names := make([]string, 0, len(found))
		for _, i := range found {
			names = append(names, i.Name)
		}
		resp.Diagnostics.AddError("No single image matches",
			fmt.Sprintf("%d match (%s). Narrow it with name or gpu_model.", len(found), strings.Join(names, ", ")))
		return
	}
	i := found[0]
	m.ID, m.Name = types.StringValue(i.Id), types.StringValue(i.Name)
	models, diags := types.ListValueFrom(ctx, types.StringType, i.GpuModels)
	resp.Diagnostics.Append(diags...)
	m.GPUModels = models
	m.LoginUser = types.StringValue(deref(i.LoginUser))
	m.MinDiskGB = types.Int64Value(int64(deref(i.MinDiskGb)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
