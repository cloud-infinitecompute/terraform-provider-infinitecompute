package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is an in-memory public API: enough of /servers, /volumes, /ssh-keys,
// /server-types and /images for the provider's unit tests. New servers and
// volumes report a transitional status on the first read, like the real API.
type fakeAPI struct {
	mu          sync.Mutex
	servers     map[string]map[string]any
	volumes     map[string]map[string]any
	keys        map[string]map[string]any
	ssh         map[string]bool
	n           int
	idempotency map[string]string
	creates     int
	keyHeader   []string
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{
		servers: map[string]map[string]any{}, volumes: map[string]map[string]any{},
		keys: map[string]map[string]any{}, ssh: map[string]bool{}, idempotency: map[string]string{},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "request_id": "req-test"}})
}

func (f *fakeAPI) id(prefix string) string {
	f.n++
	return fmt.Sprintf("%s-%04d", prefix, f.n)
}

// advance moves a transitional status on, once per read.
func advance(obj map[string]any, from, to string) {
	if obj["status"] == from {
		obj["status"] = to
	}
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer ic_ck_test" {
		fail(w, 401, "unauthenticated", "bad key")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	route := r.Method + " " + parts[0]
	if len(parts) > 1 {
		route += "/{id}"
	}
	if len(parts) > 2 {
		route += "/" + parts[2]
	}
	switch route {
	case "GET server-types":
		writeJSON(w, 200, map[string]any{"data": []any{
			map[string]any{"id": "fl-ada", "name": "ada-1x", "vcpus": 32, "ram_gb": 128.0,
				"gpu":  map[string]any{"model": "rtx6000a", "name": "NVIDIA RTX 6000 Ada Generation", "count": 1, "vram_gb": 48},
				"site": map[string]any{"id": "site-a", "name": "Site A"}, "price": map[string]any{"amount": "2.25", "currency": "USD", "per": "hour"}},
			map[string]any{"id": "fl-cpu", "name": "cpu-2x", "vcpus": 2, "ram_gb": 4.0,
				"site": map[string]any{"id": "site-b", "name": "Site B"}},
		}, "next_cursor": nil})
	case "GET images":
		writeJSON(w, 200, map[string]any{"data": []any{
			map[string]any{"id": "img-ada", "name": "ada-image", "status": "active", "visibility": "public", "gpu_models": []string{"rtx6000a"}, "login_user": "ubuntu", "min_disk_gb": 45},
			map[string]any{"id": "img-cpu", "name": "ubuntu-22.04", "status": "active", "visibility": "public", "gpu_models": []string{"none"}},
		}, "next_cursor": nil})
	case "GET ssh-keys":
		var data []any
		for _, k := range f.keys {
			data = append(data, k)
		}
		writeJSON(w, 200, map[string]any{"data": data, "next_cursor": nil})
	case "POST ssh-keys":
		name := body["name"].(string)
		if _, ok := f.keys[name]; ok {
			fail(w, 409, "conflict", "exists")
			return
		}
		f.keys[name] = map[string]any{"name": name, "type": "ssh", "fingerprint": "fp:" + name, "public_key": body["public_key"]}
		writeJSON(w, 201, f.keys[name])
	case "DELETE ssh-keys/{id}":
		delete(f.keys, parts[1])
		w.WriteHeader(204)
	case "POST servers":
		key := r.Header.Get("Idempotency-Key")
		f.keyHeader = append(f.keyHeader, key)
		if sid, ok := f.idempotency[key]; ok && key != "" {
			writeJSON(w, 202, f.servers[sid])
			return
		}
		f.creates++
		sid := f.id("srv")
		f.idempotency[key] = sid
		f.servers[sid] = map[string]any{
			"id": sid, "name": body["name"], "status": "building", "server_type": body["server_type"],
			"image_id": body["image"], "ssh_key": body["ssh_key"], "site": map[string]any{"id": "site-a", "name": "Site A"},
			"addresses": []any{map[string]any{"network": "net-a", "ip": "10.0.0.9", "version": 4}},
		}
		writeJSON(w, 202, f.servers[sid])
	case "GET servers/{id}":
		s, ok := f.servers[parts[1]]
		if !ok {
			fail(w, 404, "not_found", "Not found.")
			return
		}
		advance(s, "building", "active")
		if s["status"] == "deleting" {
			delete(f.servers, parts[1])
		}
		writeJSON(w, 200, s)
	case "DELETE servers/{id}":
		s, ok := f.servers[parts[1]]
		if !ok {
			fail(w, 404, "not_found", "Not found.")
			return
		}
		s["status"] = "deleting"
		for _, v := range f.volumes {
			if v["attached_to"].([]string) != nil && len(v["attached_to"].([]string)) > 0 && v["attached_to"].([]string)[0] == parts[1] {
				v["attached_to"], v["status"] = []string{}, "available"
			}
		}
		writeJSON(w, 202, s)
	case "GET servers/{id}/ssh", "PUT servers/{id}/ssh", "DELETE servers/{id}/ssh":
		if _, ok := f.servers[parts[1]]; !ok {
			fail(w, 404, "not_found", "Not found.")
			return
		}
		if r.Method == "PUT" {
			f.ssh[parts[1]] = true
		} else if r.Method == "DELETE" {
			f.ssh[parts[1]] = false
		}
		if f.ssh[parts[1]] {
			writeJSON(w, 200, map[string]any{"state": "open", "host": "gateway.test", "port": 21004, "user": "ubuntu", "command": "ssh -p 21004 ubuntu@gateway.test"})
		} else {
			writeJSON(w, 200, map[string]any{"state": "closed"})
		}
	case "POST volumes":
		vid := f.id("vol")
		f.volumes[vid] = map[string]any{"id": vid, "name": body["name"], "size_gb": body["size_gb"], "status": "creating",
			"site": map[string]any{"id": body["site"], "name": body["site"]}, "attached_to": []string{}}
		writeJSON(w, 202, f.volumes[vid])
	case "GET volumes/{id}":
		v, ok := f.volumes[parts[1]]
		if !ok {
			fail(w, 404, "not_found", "Not found.")
			return
		}
		advance(v, "creating", "available")
		if v["status"] == "deleting" {
			delete(f.volumes, parts[1])
		}
		writeJSON(w, 200, v)
	case "DELETE volumes/{id}":
		v, ok := f.volumes[parts[1]]
		if !ok {
			fail(w, 404, "not_found", "Not found.")
			return
		}
		if len(v["attached_to"].([]string)) > 0 {
			fail(w, 409, "conflict", "The volume is attached: detach it first.")
			return
		}
		v["status"] = "deleting"
		writeJSON(w, 202, v)
	case "POST volumes/{id}/attach":
		v := f.volumes[parts[1]]
		v["attached_to"], v["status"] = []string{body["server_id"].(string)}, "in_use"
		writeJSON(w, 202, v)
	case "POST volumes/{id}/detach":
		v := f.volumes[parts[1]]
		if len(v["attached_to"].([]string)) == 0 {
			fail(w, 409, "conflict", "The volume isn't attached.")
			return
		}
		v["attached_to"], v["status"] = []string{}, "available"
		writeJSON(w, 202, v)
	default:
		fail(w, 404, "not_found", "no route "+route)
	}
}
