package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloud-infinitecompute/terraform-provider-infinitecompute/internal/client"
)

func TestErrorsArePublicEnvelopes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"Not found.","request_id":"req-1"}}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "k", "test")
	_, err := c.Server(context.Background(), "x")
	var e *Error
	if !errors.As(err, &e) || e.Code != "not_found" || e.RequestID != "req-1" || !IsNotFound(err) {
		t.Fatalf("got %v", err)
	}
	if err.Error() != "Not found. (not_found, request req-1)" {
		t.Fatalf("message %q", err.Error())
	}
}

func TestNonJSONErrorStillErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, "<html>bad gateway</html>")
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "k", "test")
	if _, err := c.Server(context.Background(), "x"); err == nil || IsNotFound(err) {
		t.Fatalf("got %v", err)
	}
}

func TestListsFollowCursorsAndSendTheKey(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.URL.Query().Get("limit") != "200" {
			t.Errorf("limit %q", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"data":[{"name":"a","type":"ssh","fingerprint":"f","public_key":"k"}],"next_cursor":"c2"}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"name":"b","type":"ssh","fingerprint":"f","public_key":"k"}],"next_cursor":null}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "ic_ck_x", "test")
	keys, err := c.SSHKeys(context.Background())
	if err != nil || len(keys) != 2 || keys[1].Name != "b" {
		t.Fatalf("got %v %v", keys, err)
	}
	if auth[0] != "Bearer ic_ck_x" {
		t.Fatalf("auth %q", auth[0])
	}
}

func TestCreateSendsTheIdempotencyKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "k-1" {
			t.Errorf("Idempotency-Key %q", r.Header.Get("Idempotency-Key"))
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"id":"s1","name":"n","status":"building"}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "k", "test")
	s, err := c.CreateServer(context.Background(), client.ServerCreate{Name: "n"}, "k-1")
	if err != nil || s.Id != "s1" {
		t.Fatalf("got %v %v", s, err)
	}
}
