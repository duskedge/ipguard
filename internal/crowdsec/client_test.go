package crowdsec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeLAPI(t *testing.T) (*httptest.Server, *atomic.Int32, *[]map[string]any, *[]string) {
	t.Helper()
	var logins atomic.Int32
	var posted []map[string]any
	var deleted []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watchers/login", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["machine_id"] != "ipguard" || in["password"] != "secret" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"incorrect Username or Password"}`))
			return
		}
		logins.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "token": "tok", "expire": time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	})
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Method == http.MethodPost {
			var in []map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			posted = append(posted, in...)
			_, _ = w.Write([]byte(`["1"]`))
			return
		}
		if r.URL.Query().Get("include_capi") != "true" {
			t.Errorf("应当包含 CAPI")
		}
		_, _ = w.Write([]byte(`[
		 {"id":1,"created_at":"2026-11-03T10:00:00Z","decisions":[
		   {"id":11,"origin":"crowdsec","type":"ban","scope":"Ip","value":"1.2.3.4","duration":"3h12m","scenario":"crowdsecurity/http-probing"},
		   {"id":12,"origin":"crowdsec","type":"ban","scope":"Ip","value":"1.2.3.5","duration":"-5m","scenario":"crowdsecurity/http-probing"}]},
		 {"id":2,"created_at":"2026-11-03T09:00:00Z","decisions":[
		   {"id":21,"origin":"CAPI","type":"ban","scope":"Ip","value":"5.6.7.8","duration":"150h0m1.5s","scenario":"crowdsecurity/ssh-bf"},
		   {"id":22,"origin":"lists","type":"ban","scope":"Range","value":"9.9.9.0/24","duration":"20h","scenario":"firehol"}]}
		]`))
	})
	mux.HandleFunc("/v1/decisions/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/v1/decisions/"))
		_, _ = w.Write([]byte(`{"nbDeleted":"1"}`))
	})
	return httptest.NewServer(mux), &logins, &posted, &deleted
}

func TestActiveDecisions(t *testing.T) {
	srv, logins, _, _ := fakeLAPI(t)
	defer srv.Close()
	c := New(srv.URL, "ipguard", "secret", 5*time.Second)
	ds, err := c.ActiveDecisions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 3 {
		t.Fatalf("过期决策应被滤掉，得到 %d 条", len(ds))
	}
	if ds[0].CreatedAt.IsZero() {
		t.Fatal("创建时间应取自 alert")
	}
	// 第二次调用复用 token
	if _, err := c.ActiveDecisions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 1 {
		t.Fatalf("token 应复用，登录了 %d 次", logins.Load())
	}
}

func TestAddAndDelete(t *testing.T) {
	srv, _, posted, deleted := fakeLAPI(t)
	defer srv.Close()
	c := New(srv.URL, "ipguard", "secret", 5*time.Second)
	err := c.AddBan(context.Background(), BanRequest{Scope: "Range", Value: "203.0.113.0/24", Duration: 7 * 24 * time.Hour, Reason: "manual-ipguard", Origin: "cscli"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*posted) != 1 {
		t.Fatal("应提交一个 alert")
	}
	d := (*posted)[0]["decisions"].([]any)[0].(map[string]any)
	if d["duration"] != "168h" || d["scope"] != "Range" || d["value"] != "203.0.113.0/24" || d["origin"] != "cscli" {
		t.Fatalf("决策字段不对: %v", d)
	}
	if err := c.DeleteDecision(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if len(*deleted) != 1 || (*deleted)[0] != "42" {
		t.Fatalf("删除路径不对: %v", *deleted)
	}
}

func TestBadCredentials(t *testing.T) {
	srv, _, _, _ := fakeLAPI(t)
	defer srv.Close()
	c := New(srv.URL, "ipguard", "wrong", 5*time.Second)
	err := c.Ping(context.Background())
	var ae *APIError
	if err == nil || !asAPI(err, &ae) || ae.Status != http.StatusForbidden {
		t.Fatalf("应返回 403: %v", err)
	}
}

func asAPI(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}

func TestFormatDuration(t *testing.T) {
	if formatDuration(90*time.Minute) != "90m" || formatDuration(24*time.Hour) != "24h" {
		t.Fatal("时长格式不对")
	}
}
