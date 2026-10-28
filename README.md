# terraform-provider-infinitecompute

Terraform provider for [Infinite Compute](https://infinitecompute.ai): GPU servers, volumes and SSH access through the public API (`/api/public/v1`). Published on the Terraform Registry as `cloud-infinitecompute/infinitecompute`.

```hcl
terraform {
  required_providers {
    infinitecompute = { source = "cloud-infinitecompute/infinitecompute" }
  }
}

provider "infinitecompute" {} # INFINITECOMPUTE_API_KEY, or api_key = "ic_ck_…"

data "infinitecompute_server_type" "gpu" {
  gpu_model = "rtx6000a" # the GPU models and sites you can use are in your console
}

data "infinitecompute_image" "gpu" {
  gpu_model = data.infinitecompute_server_type.gpu.gpu_model
}

resource "infinitecompute_ssh_key" "me" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "infinitecompute_server" "train" {
  name        = "train"
  server_type = data.infinitecompute_server_type.gpu.name
  image       = data.infinitecompute_image.gpu.id
  ssh_key     = infinitecompute_ssh_key.me.name
  ssh_access  = { enabled = true }
}

resource "infinitecompute_volume" "data" {
  name    = "datasets"
  size_gb = 200
  site    = data.infinitecompute_server_type.gpu.site
}

resource "infinitecompute_volume_attachment" "data" {
  server_id = infinitecompute_server.train.id
  volume_id = infinitecompute_volume.data.id
}

output "ssh" { value = infinitecompute_server.train.ssh_access.command }
```

| | |
|---|---|
| Resources | `infinitecompute_server`, `infinitecompute_volume`, `infinitecompute_volume_attachment`, `infinitecompute_ssh_key` |
| Data sources | `infinitecompute_server_type` (by name, `gpu_model`, `site`), `infinitecompute_image` (by name, `gpu_model`) |
| Waits | servers until `active` (default 20 min, `timeouts = { create = "30m" }`), volumes until available, deletes until gone |
| Retries | every create sends a fresh `Idempotency-Key` |
| Import | servers and volumes by id, SSH keys by name, attachments by `<server_id>/<volume_id>` |

Changing anything on a server except `ssh_access` replaces it. A server that fails to build is saved and marked tainted, so `apply` again replaces it and `destroy` removes it.

## Develop

```bash
go generate ./...   # client from api/openapi-v1.json (the API's published contract)
go test ./...       # unit tests: Terraform runs against an in-memory fake of the API
go build -o ~/.terraform.d/dev/terraform-provider-infinitecompute .
```

Try a local build with a `dev_overrides` CLI config pointing at that directory. The client is generated with `oapi-codegen` after `tools/openapi30` rewrites the API's OpenAPI 3.1 document as 3.0.

## Verifying releases

Release checksums are signed with the key in [`SIGNING-KEY.asc`](SIGNING-KEY.asc) (fingerprint `0F3C 1EFB 76CE DC49 51E2  F948 5D6E 0302 E4F8 F159`); Terraform checks it when it installs the provider from the Registry.
