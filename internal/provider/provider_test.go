package provider

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"infinitecompute": providerserver.NewProtocol6WithError(New("test")()),
}

func config(url string, ssh bool) string {
	return fmt.Sprintf(`
provider "infinitecompute" {
  api_url = %q
  api_key = "ic_ck_test"
}

data "infinitecompute_server_type" "ada" {
  gpu_model = "rtx6000a"
  site      = "site-a"
}

data "infinitecompute_image" "ada" {
  gpu_model = data.infinitecompute_server_type.ada.gpu_model
}

resource "infinitecompute_ssh_key" "me" {
  name       = "laptop"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest me@laptop"
}

resource "infinitecompute_server" "train" {
  name        = "train"
  server_type = data.infinitecompute_server_type.ada.name
  image       = data.infinitecompute_image.ada.id
  ssh_key     = infinitecompute_ssh_key.me.name
  ssh_access = {
    enabled = %t
  }
}

resource "infinitecompute_volume" "data" {
  name    = "data"
  size_gb = 100
  site    = data.infinitecompute_server_type.ada.site
}

resource "infinitecompute_volume_attachment" "data" {
  server_id = infinitecompute_server.train.id
  volume_id = infinitecompute_volume.data.id
}
`, url, ssh)
}

func TestProviderLifecycle(t *testing.T) {
	pollEvery = 10 * time.Millisecond
	f, srv := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.servers)+len(f.volumes)+len(f.keys) != 0 {
				return fmt.Errorf("left behind: %d servers, %d volumes, %d keys", len(f.servers), len(f.volumes), len(f.keys))
			}
			return nil
		},
		Steps: []resource.TestStep{
			{ // apply; the framework then checks that a second plan is empty
				Config: config(srv.URL, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.infinitecompute_server_type.ada", "name", "ada-1x"),
					resource.TestCheckResourceAttr("data.infinitecompute_server_type.ada", "price_per_hour", "2.25"),
					resource.TestCheckResourceAttr("data.infinitecompute_image.ada", "id", "img-ada"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "status", "active"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "site", "site-a"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "ip", "10.0.0.9"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "delete_disk_with_server", "true"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "ssh_access.port", "21004"),
					resource.TestCheckResourceAttr("infinitecompute_server.train", "ssh_access.user", "ubuntu"),
					resource.TestCheckResourceAttr("infinitecompute_volume.data", "status", "available"),
					resource.TestCheckResourceAttr("infinitecompute_ssh_key.me", "fingerprint", "fp:laptop"),
					func(*terraform.State) error {
						if len(f.keyHeader) != 1 || len(f.keyHeader[0]) != 36 {
							return fmt.Errorf("want one create with a UUID Idempotency-Key, got %q", f.keyHeader)
						}
						return nil
					},
				),
			},
			{ // closing SSH changes the server in place
				Config: config(srv.URL, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("infinitecompute_server.train", "ssh_access.enabled", "false"),
					resource.TestCheckNoResourceAttr("infinitecompute_server.train", "ssh_access.port"),
					func(*terraform.State) error {
						if f.creates != 1 {
							return fmt.Errorf("the server was replaced (%d creates)", f.creates)
						}
						return nil
					},
				),
			},
			{
				ResourceName:            "infinitecompute_server.train",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ssh_access", "timeouts"},
			},
			{ResourceName: "infinitecompute_volume.data", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
			{ResourceName: "infinitecompute_volume_attachment.data", ImportState: true, ImportStateVerify: true},
			{ResourceName: "infinitecompute_ssh_key.me", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestNoAPIKey(t *testing.T) {
	t.Setenv(envAPIKey, "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `data "infinitecompute_image" "x" { name = "y" }`,
			ExpectError: regexp.MustCompile("No API key"),
		}},
	})
}

func TestAmbiguousDataSource(t *testing.T) {
	_, srv := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "infinitecompute" {
  api_url = %q
  api_key = "ic_ck_test"
}
data "infinitecompute_server_type" "any" {}
`, srv.URL),
			ExpectError: regexp.MustCompile(`2 match \(ada-1x, cpu-2x\)`),
		}},
	})
}
