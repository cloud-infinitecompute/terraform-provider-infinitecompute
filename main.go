// Command terraform-provider-infinitecompute is the Terraform provider for
// Infinite Compute's public API (W2 A-7).
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/provider"
)

// version is set by the release build (goreleaser ldflags).
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/cloud-infinitecompute/infinitecompute",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
