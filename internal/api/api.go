// Package api is a small, typed layer over the generated client
// (internal/client): one method per call the provider makes, the API's error
// envelope as a Go error, and every list paged through to the end.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/client"
)

const (
	DefaultURL = "https://cloud.infinitecompute.ai/api/public/v1"
	pageSize   = 200
)

// Error is the API's error envelope: {"error": {"code", "message", "request_id"}}.
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("%s (%s, request %s)", e.Message, e.Code, e.RequestID)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// IsNotFound reports whether err is the API's not_found.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

type Client struct {
	c *client.ClientWithResponses
}

// New returns a client for baseURL that sends the API key on every request.
func New(baseURL, apiKey, userAgent string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	c, err := client.NewClientWithResponses(
		baseURL,
		client.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
		client.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+apiKey)
			r.Header.Set("User-Agent", userAgent)
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Client{c: c}, nil
}

// check turns a response into an error unless its status is one of ok.
func check(status int, body []byte, err error, ok ...int) error {
	if err != nil {
		return err
	}
	for _, s := range ok {
		if status == s {
			return nil
		}
	}
	var env client.ErrorResponse
	if json.Unmarshal(body, &env) == nil && env.Error.Code != "" {
		return &Error{status, env.Error.Code, env.Error.Message, env.Error.RequestId}
	}
	return &Error{Status: status, Code: "http_error", Message: fmt.Sprintf("unexpected HTTP %d", status)}
}

func limit() *int { n := pageSize; return &n }

// --- reads -------------------------------------------------------------------------

func (a *Client) Me(ctx context.Context) (*client.Me, error) {
	r, err := a.c.MeMeGetWithResponse(ctx)
	if err := check(code(r, err), body(r, err), err, 200); err != nil {
		return nil, err
	}
	return decode[client.Me](r.Body)
}

func (a *Client) ServerTypes(ctx context.Context) ([]client.ServerType, error) {
	var out []client.ServerType
	var cursor *string
	for {
		r, err := a.c.ListServerTypesServerTypesGetWithResponse(ctx,
			&client.ListServerTypesServerTypesGetParams{Limit: limit(), Cursor: cursor})
		if err := check(code(r, err), body(r, err), err, 200); err != nil {
			return nil, err
		}
		page, err := decode[client.ServerTypeList](r.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if cursor = page.NextCursor; cursor == nil {
			return out, nil
		}
	}
}

func (a *Client) Images(ctx context.Context) ([]client.Image, error) {
	var out []client.Image
	var cursor *string
	for {
		r, err := a.c.ListImagesImagesGetWithResponse(ctx,
			&client.ListImagesImagesGetParams{Limit: limit(), Cursor: cursor})
		if err := check(code(r, err), body(r, err), err, 200); err != nil {
			return nil, err
		}
		page, err := decode[client.ImageList](r.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if cursor = page.NextCursor; cursor == nil {
			return out, nil
		}
	}
}

func (a *Client) SSHKeys(ctx context.Context) ([]client.SshKey, error) {
	var out []client.SshKey
	var cursor *string
	for {
		r, err := a.c.ListSshKeysSshKeysGetWithResponse(ctx,
			&client.ListSshKeysSshKeysGetParams{Limit: limit(), Cursor: cursor})
		if err := check(code(r, err), body(r, err), err, 200); err != nil {
			return nil, err
		}
		page, err := decode[client.SshKeyList](r.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if cursor = page.NextCursor; cursor == nil {
			return out, nil
		}
	}
}

func (a *Client) Server(ctx context.Context, id string) (*client.Server, error) {
	r, err := a.c.GetServerServersServerIdGetWithResponse(ctx, id)
	if err := check(code(r, err), body(r, err), err, 200); err != nil {
		return nil, err
	}
	return decode[client.Server](r.Body)
}

func (a *Client) Volume(ctx context.Context, id string) (*client.Volume, error) {
	r, err := a.c.GetVolumeVolumesVolumeIdGetWithResponse(ctx, id)
	if err := check(code(r, err), body(r, err), err, 200); err != nil {
		return nil, err
	}
	return decode[client.Volume](r.Body)
}

func (a *Client) SSHAccess(ctx context.Context, serverID string) (*client.SshAccess, error) {
	r, err := a.c.GetSshServersServerIdSshGetWithResponse(ctx, serverID)
	if err := check(code(r, err), body(r, err), err, 200); err != nil {
		return nil, err
	}
	return decode[client.SshAccess](r.Body)
}

// --- writes ------------------------------------------------------------------------

func (a *Client) CreateServer(ctx context.Context, in client.ServerCreate, idempotencyKey string) (*client.Server, error) {
	r, err := a.c.CreateServerServersPostWithResponse(ctx,
		&client.CreateServerServersPostParams{IdempotencyKey: &idempotencyKey}, in)
	if err := check(code(r, err), body(r, err), err, 202); err != nil {
		return nil, err
	}
	return decode[client.Server](r.Body)
}

func (a *Client) DeleteServer(ctx context.Context, id string) error {
	r, err := a.c.DeleteServerServersServerIdDeleteWithResponse(ctx, id)
	return check(code(r, err), body(r, err), err, 202)
}

func (a *Client) OpenSSH(ctx context.Context, serverID string) (*client.SshAccess, error) {
	r, err := a.c.OpenSshServersServerIdSshPutWithResponse(ctx, serverID)
	if err := check(code(r, err), body(r, err), err, 200); err != nil {
		return nil, err
	}
	return decode[client.SshAccess](r.Body)
}

func (a *Client) CloseSSH(ctx context.Context, serverID string) error {
	r, err := a.c.CloseSshServersServerIdSshDeleteWithResponse(ctx, serverID)
	return check(code(r, err), body(r, err), err, 200)
}

func (a *Client) CreateSSHKey(ctx context.Context, name, publicKey string) (*client.SshKey, error) {
	r, err := a.c.CreateSshKeySshKeysPostWithResponse(ctx,
		client.SshKeyCreate{Name: name, PublicKey: publicKey})
	if err := check(code(r, err), body(r, err), err, 201); err != nil {
		return nil, err
	}
	return decode[client.SshKey](r.Body)
}

func (a *Client) DeleteSSHKey(ctx context.Context, name string) error {
	r, err := a.c.DeleteSshKeySshKeysNameDeleteWithResponse(ctx, name)
	return check(code(r, err), body(r, err), err, 204)
}

func (a *Client) CreateVolume(ctx context.Context, in client.VolumeCreate, idempotencyKey string) (*client.Volume, error) {
	r, err := a.c.CreateVolumeVolumesPostWithResponse(ctx,
		&client.CreateVolumeVolumesPostParams{IdempotencyKey: &idempotencyKey}, in)
	if err := check(code(r, err), body(r, err), err, 202); err != nil {
		return nil, err
	}
	return decode[client.Volume](r.Body)
}

func (a *Client) DeleteVolume(ctx context.Context, id string) error {
	r, err := a.c.DeleteVolumeVolumesVolumeIdDeleteWithResponse(ctx, id)
	return check(code(r, err), body(r, err), err, 202)
}

func (a *Client) AttachVolume(ctx context.Context, volumeID, serverID string) error {
	r, err := a.c.AttachVolumeVolumesVolumeIdAttachPostWithResponse(ctx, volumeID,
		client.VolumeAttach{ServerId: serverID})
	return check(code(r, err), body(r, err), err, 202)
}

func (a *Client) DetachVolume(ctx context.Context, volumeID string) error {
	r, err := a.c.DetachVolumeVolumesVolumeIdDetachPostWithResponse(ctx, volumeID)
	return check(code(r, err), body(r, err), err, 202)
}

// --- helpers -----------------------------------------------------------------------

type response interface {
	StatusCode() int
}

func code[R response](r R, err error) int {
	if err != nil {
		return 0
	}
	return r.StatusCode()
}

// body is the raw response body: every generated response has a Body field.
func body[R response](r R, err error) []byte {
	if err != nil {
		return nil
	}
	v := reflect.Indirect(reflect.ValueOf(r))
	if f := v.FieldByName("Body"); f.IsValid() {
		if b, ok := f.Interface().([]byte); ok {
			return b
		}
	}
	return nil
}

func decode[T any](b []byte) (*T, error) {
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decoding the API's response: %w", err)
	}
	return &out, nil
}
