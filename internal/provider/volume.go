package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/client"
)

const volumeTimeout = 10 * time.Minute

// --- infinitecompute_volume ----------------------------------------------------------

type volumeResource struct{ c *api.Client }

type volumeModel struct {
	ID       types.String   `tfsdk:"id"`
	Name     types.String   `tfsdk:"name"`
	SizeGB   types.Int64    `tfsdk:"size_gb"`
	Site     types.String   `tfsdk:"site"`
	Status   types.String   `tfsdk:"status"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func newVolumeResource() resource.Resource { return &volumeResource{} }

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A data volume at a site, on that site's backed-up storage. Attach it with infinitecompute_volume_attachment.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"size_gb": schema.Int64Attribute{
				Required: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"site":     schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "Site id, as a server type reports it (infinitecompute_server_type.site). Attach only to servers there."},
			"status":   schema.StringAttribute{Computed: true, Description: "creating, available, in_use, busy, deleting or error."},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *volumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func fillVolume(m *volumeModel, v *client.Volume) {
	m.ID = types.StringValue(v.Id)
	m.Name = types.StringValue(v.Name)
	m.SizeGB = types.Int64Value(int64(v.SizeGb))
	m.Status = types.StringValue(v.Status)
	if v.Site != nil {
		m.Site = types.StringValue(v.Site.Id)
	}
}

// waitVolume waits until the volume's status is one of want; error fails it.
func waitVolume(ctx context.Context, c *api.Client, id string, timeout time.Duration, want ...string) (*client.Volume, error) {
	var last *client.Volume
	err := waitFor(ctx, timeout, "the volume to be "+strings.Join(want, " or "), func() (bool, error) {
		v, err := c.Volume(ctx, id)
		if err != nil {
			return false, err
		}
		last = v
		if v.Status == "error" {
			return false, fmt.Errorf("the volume is in error")
		}
		return slices.Contains(want, v.Status), nil
	})
	return last, err
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m volumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := m.Timeouts.Create(ctx, volumeTimeout)
	resp.Diagnostics.Append(d...)
	v, err := r.c.CreateVolume(ctx, client.VolumeCreate{
		Name: m.Name.ValueString(), SizeGb: int(m.SizeGB.ValueInt64()), Site: m.Site.ValueString(),
	}, uuid.NewString())
	if err != nil {
		resp.Diagnostics.AddError("Could not create the volume", err.Error())
		return
	}
	fillVolume(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	last, err := waitVolume(ctx, r.c, v.Id, timeout, "available")
	if last != nil {
		fillVolume(&m, last)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if err != nil {
		resp.Diagnostics.AddError("The volume didn't become available", err.Error())
	}
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.c.Volume(ctx, m.ID.ValueString())
	if api.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read the volume", err.Error())
		return
	}
	fillVolume(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *volumeResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// every attribute requires replacement
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	timeout, d := m.Timeouts.Delete(ctx, volumeTimeout)
	resp.Diagnostics.Append(d...)
	id := m.ID.ValueString()
	if err := r.c.DeleteVolume(ctx, id); err != nil {
		if !api.IsNotFound(err) {
			resp.Diagnostics.AddError("Could not delete the volume", err.Error())
		}
		return
	}
	err := waitFor(ctx, timeout, "the volume to be deleted", func() (bool, error) {
		_, err := r.c.Volume(ctx, id)
		if api.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		resp.Diagnostics.AddError("The volume wasn't deleted", err.Error())
	}
}

func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// --- infinitecompute_volume_attachment -----------------------------------------------

type attachmentResource struct{ c *api.Client }

type attachmentModel struct {
	ID       types.String `tfsdk:"id"`
	ServerID types.String `tfsdk:"server_id"`
	VolumeID types.String `tfsdk:"volume_id"`
}

func newVolumeAttachmentResource() resource.Resource { return &attachmentResource{} }

func (r *attachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *attachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Attaches a volume to a server at the same site. Import with <server_id>/<volume_id>.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"server_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"volume_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
		},
	}
}

func (r *attachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *attachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m attachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sid, vid := m.ServerID.ValueString(), m.VolumeID.ValueString()
	if err := r.c.AttachVolume(ctx, vid, sid); err != nil {
		resp.Diagnostics.AddError("Could not attach the volume", err.Error())
		return
	}
	m.ID = types.StringValue(sid + "/" + vid)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	err := waitFor(ctx, volumeTimeout, "the volume to attach", func() (bool, error) {
		v, err := r.c.Volume(ctx, vid)
		if err != nil {
			return false, err
		}
		if v.Status == "error" {
			return false, fmt.Errorf("the volume is in error")
		}
		return v.Status == "in_use" && slices.Contains(deref(v.AttachedTo), sid), nil
	})
	if err != nil {
		resp.Diagnostics.AddError("The volume didn't attach", err.Error())
	}
}

func (r *attachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.c.Volume(ctx, m.VolumeID.ValueString())
	if api.IsNotFound(err) || (err == nil && !slices.Contains(deref(v.AttachedTo), m.ServerID.ValueString())) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read the volume", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *attachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *attachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	vid := m.VolumeID.ValueString()
	if err := r.c.DetachVolume(ctx, vid); err != nil {
		var e *api.Error
		if api.IsNotFound(err) || (asError(err, &e) && e.Code == "conflict" && strings.Contains(e.Message, "isn't attached")) {
			return
		}
		resp.Diagnostics.AddError("Could not detach the volume", err.Error())
		return
	}
	err := waitFor(ctx, volumeTimeout, "the volume to detach", func() (bool, error) {
		v, err := r.c.Volume(ctx, vid)
		if api.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return v.Status == "available", nil
	})
	if err != nil {
		resp.Diagnostics.AddError("The volume didn't detach", err.Error())
	}
}

func (r *attachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Bad import id", "Use <server_id>/<volume_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("volume_id"), parts[1])...)
}
