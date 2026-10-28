package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/api"
)

// pollEvery is a variable so tests can poll fast.
var pollEvery = 5 * time.Second

// clientFrom is the Configure step every resource and data source shares.
func clientFrom(data any, diags *diag.Diagnostics) *api.Client {
	if data == nil {
		return nil // the provider isn't configured yet (validation)
	}
	c, ok := data.(*api.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *api.Client, got %T", data))
	}
	return c
}

// waitFor polls check every pollEvery until it reports done, fails, or the
// context ends. check returns done=true when the wait is over.
func waitFor(ctx context.Context, timeout time.Duration, what string, check func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out after %s waiting for %s", timeout, what)
		case <-time.After(pollEvery):
		}
	}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func asError(err error, target **api.Error) bool { return errors.As(err, target) }
