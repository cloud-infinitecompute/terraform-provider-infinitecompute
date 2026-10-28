package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
)

// infinitecompute_ssh_key: one of the key owner's SSH public keys (keys belong
// to a person, not a project). Changing anything replaces it; the id is the name.
type sshKeyResource struct{ c *api.Client }

type sshKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
}

func newSSHKeyResource() resource.Resource { return &sshKeyResource{} }

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "An SSH public key, used to log in to servers created with it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The key's name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name":        schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "Unique among your keys."},
			"public_key":  schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "OpenSSH format, e.g. the contents of ~/.ssh/id_ed25519.pub."},
			"fingerprint": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.c.CreateSSHKey(ctx, m.Name.ValueString(), strings.TrimSpace(m.PublicKey.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Could not add the SSH key", err.Error())
		return
	}
	m.ID = types.StringValue(key.Name)
	m.Fingerprint = types.StringValue(key.Fingerprint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	keys, err := r.c.SSHKeys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Could not read SSH keys", err.Error())
		return
	}
	for _, k := range keys {
		if k.Name != m.ID.ValueString() {
			continue
		}
		m.Name = types.StringValue(k.Name)
		m.Fingerprint = types.StringValue(k.Fingerprint)
		if m.PublicKey.IsNull() { // import: take the key as stored
			m.PublicKey = types.StringValue(k.PublicKey)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *sshKeyResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// every attribute requires replacement
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if err := r.c.DeleteSSHKey(ctx, m.ID.ValueString()); err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete the SSH key", err.Error())
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
