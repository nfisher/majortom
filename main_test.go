package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Test_get_should_not_be_allowed_method(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	addOwnerLabel(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("w.Code=%v, want StatusMethodNotAllowed", w.Code)
	}
}

func Test_non_json_content_type_should_be_invalid(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	r.Header.Set("Content-Type", "text/html")
	w := httptest.NewRecorder()
	addOwnerLabel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("w.Code=%v, want StatusBadRequest", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "invalid content-type") {
		t.Errorf("w.Body starts with <%v>, want `invalid content-type`", w.Body.String())
	}
}

func Test_bad_body_should_be_bad_request(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	r.Header.Set("Content-Type", ApplicationJson)
	w := httptest.NewRecorder()
	addOwnerLabel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("w.Code=%v, want StatusBadRequest", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "nil admission request") {
		t.Errorf("w.Body starts with <%v>, want `nil admission request`", w.Body.String())
	}
}
