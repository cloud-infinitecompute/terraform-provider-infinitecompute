package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/client"
)

const (
	defaultCreateTimeout = 20 * time.Minute
	defaultDeleteTimeout = 10 * time.Minute
)

// infinitecompute_server: POST /servers with the console's launch rules, then
// wait for active. Everything but ssh_access replaces the server; ssh_access
// opens or closes SSH access in place.
type serverResource struct{ c *api.Client }

type serverModel struct {
	ID                   types.String   `tfsdk:"id"`
	Name                 types.String   `tfsdk:"name"`
	ServerType           types.String   `tfsdk:"server_type"`
	Image                types.String   `tfsdk:"image"`
	SSHKey               types.String   `tfsdk:"ssh_key"`
	DiskGB               types.Int64    `tfsdk:"disk_gb"`
	UserData             types.String   `tfsdk:"user_data"`
	DeleteDiskWithServer types.Bool     `tfsdk:"delete_disk_with_server"`
	SSHAccess            types.Object   `tfsdk:"ssh_access"`
	Status               types.String   `tfsdk:"status"`
	Site                 types.String   `tfsdk:"site"`
	IP                   types.String   `tfsdk:"ip"`
	ImageID              types.String   `tfsdk:"image_id"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

var sshAccessTypes = map[string]attr.Type{
	"enabled": types.BoolType,
	"host":    types.StringType,
	"port":    types.Int64Type,
	"user":    types.StringType,
	"command": types.StringType,
}

func newServerResource() resource.Resource { return &serverResource{} }

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "A GPU or CPU server. It boots from a new volume on its site's backed-up storage.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"name":        schema.StringAttribute{Required: true, PlanModifiers: replace},
			"server_type": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "A server type's name or id; see the infinitecompute_server_type data source."},
			"image":       schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "An image id (or name) built for the type's GPU; see the infinitecompute_image data source."},
			"ssh_key":     schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "The name of one of your SSH keys."},
			"disk_gb": schema.Int64Attribute{
				Optional: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				Description: "System disk size in GB. Default 50, or the image's minimum if larger.",
			},
			"user_data": schema.StringAttribute{Optional: true, PlanModifiers: replace, Description: "cloud-init user data, as text."},
			"delete_disk_with_server": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
				Description:   "Delete the system disk with the server. Default true.",
			},
			"ssh_access": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "SSH access through the platform's SSH gateway. Changing enabled opens or closes it in place.",
				Attributes: map[string]schema.Attribute{
					"enabled": schema.BoolAttribute{Required: true},
					"host":    schema.StringAttribute{Computed: true},
					"port":    schema.Int64Attribute{Computed: true},
					"user":    schema.StringAttribute{Computed: true},
					"command": schema.StringAttribute{Computed: true, Description: "A ready-made ssh command line."},
				},
			},
			"status":   schema.StringAttribute{Computed: true, Description: "building, active, busy, stopped, parked, deleting or error."},
			"site":     schema.StringAttribute{Computed: true, PlanModifiers: keep, Description: "Site id, as a server type reports it."},
			"ip":       schema.StringAttribute{Computed: true, PlanModifiers: keep, Description: "Private IPv4 address."},
			"image_id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan keeps ssh_access's computed values while enabled doesn't change,
// and marks them unknown when it does.
func (r *serverResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() || plan.SSHAccess.IsNull() || plan.SSHAccess.IsUnknown() {
		return
	}
	want := sshEnabled(plan.SSHAccess)
	if !state.SSHAccess.IsNull() && !state.SSHAccess.IsUnknown() && sshEnabled(state.SSHAccess) == want {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ssh_access"), state.SSHAccess)...)
		return
	}
	if !want {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ssh_access"), sshObject(false, nil))...)
		return
	}
	unknown, d := types.ObjectValue(sshAccessTypes, map[string]attr.Value{
		"enabled": types.BoolValue(true),
		"host":    types.StringUnknown(),
		"port":    types.Int64Unknown(),
		"user":    types.StringUnknown(),
		"command": types.StringUnknown(),
	})
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ssh_access"), unknown)...)
}

func sshEnabled(o types.Object) bool {
	if v, ok := o.Attributes()["enabled"].(basetypes.BoolValue); ok {
		return v.ValueBool()
	}
	return false
}

func sshObject(enabled bool, a *client.SshAccess) types.Object {
	host, user, command := types.StringNull(), types.StringNull(), types.StringNull()
	port := types.Int64Null()
	if enabled && a != nil && a.State == "open" {
		host = types.StringValue(deref(a.Host))
		user = types.StringValue(deref(a.User))
		command = types.StringValue(deref(a.Command))
		port = types.Int64Value(int64(deref(a.Port)))
	}
	o, _ := types.ObjectValue(sshAccessTypes, map[string]attr.Value{
		"enabled": types.BoolValue(enabled), "host": host, "port": port, "user": user, "command": command,
	})
	return o
}

// fill copies what the API reports into the model. Inputs the user gave
// (server_type, image, ssh_key) stay as written unless empty (import).
func fill(m *serverModel, s *client.Server) {
	m.ID = types.StringValue(s.Id)
	m.Name = types.StringValue(s.Name)
	m.Status = types.StringValue(s.Status)
	m.Site = types.StringNull()
	if s.Site != nil {
		m.Site = types.StringValue(s.Site.Id)
	}
	m.IP = types.StringNull()
	for _, a := range deref(s.Addresses) {
		if a.Version == 4 {
			m.IP = types.StringValue(a.Ip)
			break
		}
	}
	m.ImageID = types.StringValue(deref(s.ImageId))
	if m.ServerType.IsNull() {
		m.ServerType = types.StringValue(deref(s.ServerType))
	}
	if m.Image.IsNull() {
		m.Image = types.StringValue(deref(s.ImageId))
	}
	if m.SSHKey.IsNull() {
		m.SSHKey = types.StringValue(deref(s.SshKey))
	}
	if m.DeleteDiskWithServer.IsNull() {
		m.DeleteDiskWithServer = types.BoolValue(true)
	}
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := m.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)

	in := client.ServerCreate{
		Name:                 m.Name.ValueString(),
		ServerType:           m.ServerType.ValueString(),
		Image:                m.Image.ValueString(),
		SshKey:               m.SSHKey.ValueString(),
		DeleteDiskWithServer: m.DeleteDiskWithServer.ValueBoolPointer(),
	}
	if !m.DiskGB.IsNull() {
		n := int(m.DiskGB.ValueInt64())
		in.DiskGb = &n
	}
	if !m.UserData.IsNull() {
		in.UserData = m.UserData.ValueStringPointer()
	}
	created, err := r.c.CreateServer(ctx, in, uuid.NewString())
	if err != nil {
		resp.Diagnostics.AddError("Could not create the server", err.Error())
		return
	}
	// Saved before waiting: if the wait fails the server is tainted, not lost.
	ssh := m.SSHAccess
	fill(&m, created)
	m.SSHAccess = ssh
	if !ssh.IsNull() {
		m.SSHAccess = sshObject(false, nil)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)

	var last *client.Server
	err = waitFor(ctx, timeout, "the server to become active", func() (bool, error) {
		s, err := r.c.Server(ctx, created.Id)
		if err != nil {
			return false, err
		}
		last = s
		switch s.Status {
		case "active":
			return true, nil
		case "error":
			return false, fmt.Errorf("the server failed: %s", deref(s.ErrorReason))
		}
		return false, nil
	})
	if last != nil {
		fill(&m, last)
	}
	if err != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		resp.Diagnostics.AddError("The server didn't become active", err.Error())
		return
	}
	if !ssh.IsNull() {
		m.SSHAccess = sshObject(false, nil)
		if sshEnabled(ssh) {
			a, err := r.c.OpenSSH(ctx, m.ID.ValueString())
			if err != nil {
				resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
				resp.Diagnostics.AddError("Could not open SSH access", err.Error())
				return
			}
			m.SSHAccess = sshObject(true, a)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.c.Server(ctx, m.ID.ValueString())
	if api.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read the server", err.Error())
		return
	}
	fill(&m, s)
	if !m.SSHAccess.IsNull() {
		a, err := r.c.SSHAccess(ctx, s.Id)
		if err != nil {
			resp.Diagnostics.AddError("Could not read SSH access", err.Error())
			return
		}
		m.SSHAccess = sshObject(a.State == "open", a)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	want := !plan.SSHAccess.IsNull() && sshEnabled(plan.SSHAccess)
	had := !state.SSHAccess.IsNull() && sshEnabled(state.SSHAccess)
	switch {
	case want && !had:
		a, err := r.c.OpenSSH(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Could not open SSH access", err.Error())
			return
		}
		plan.SSHAccess = sshObject(true, a)
	case !want && had:
		if err := r.c.CloseSSH(ctx, id); err != nil && !api.IsNotFound(err) {
			resp.Diagnostics.AddError("Could not close SSH access", err.Error())
			return
		}
		if !plan.SSHAccess.IsNull() {
			plan.SSHAccess = sshObject(false, nil)
		}
	}
	s, err := r.c.Server(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Could not read the server", err.Error())
		return
	}
	ssh := plan.SSHAccess
	fill(&plan, s)
	plan.SSHAccess = ssh
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := m.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	id := m.ID.ValueString()
	if err := r.c.DeleteServer(ctx, id); err != nil {
		if api.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Could not delete the server", err.Error())
		return
	}
	err := waitFor(ctx, timeout, "the server to be deleted", func() (bool, error) {
		_, err := r.c.Server(ctx, id)
		if api.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		resp.Diagnostics.AddError("The server wasn't deleted", err.Error())
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
