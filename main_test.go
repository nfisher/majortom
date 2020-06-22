package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if !strings.HasPrefix(w.Body.String(), "error reading response body") {
		t.Errorf("w.Body starts with <%v>, want `error reading response body`", w.Body.String())
	}
}

func Test_nil_request_should_be_bad_request(t *testing.T) {
	b, _ := json.Marshal(&v1.AdmissionReview{})
	buf := bytes.NewBuffer(b)
	r, _ := http.NewRequest(http.MethodPost, "/", buf)
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

func Test_system_namespace_request_should_be_forbidden(t *testing.T) {
	b, _ := json.Marshal(&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "kube-system"}})
	buf := bytes.NewBuffer(b)
	r, _ := http.NewRequest(http.MethodPost, "/", buf)
	r.Header.Set("Content-Type", ApplicationJson)
	w := httptest.NewRecorder()
	addOwnerLabel(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("w.Code=%v, want StatusForbidden", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "will not modify resource in kube-* namespace") {
		t.Errorf("w.Body starts with <%v>, want `will not modify resource in kube-* namespace`", w.Body.String())
	}
}

func Test_system_namespace_request_should_be_pod(t *testing.T) {
	b, _ := json.Marshal(&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: metav1.GroupVersionResource{Version: "v1", Resource: "deployment"}}})
	buf := bytes.NewBuffer(b)
	r, _ := http.NewRequest(http.MethodPost, "/", buf)
	r.Header.Set("Content-Type", ApplicationJson)
	w := httptest.NewRecorder()
	addOwnerLabel(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("w.Code=%v, want StatusBadRequest", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "resource not a v1.Pod") {
		t.Errorf("w.Body starts with <%v>, want `resource not a v1.Pod`", w.Body.String())
	}
}

func Test_isSystem_kube_public(t *testing.T) {
	actual := isSystem("kube-public")
	if actual != true {
		t.Errorf("isSystem(`kube-public`)=%v, want false", actual)
	}
}

func Test_isSystem_kube_system(t *testing.T) {
	actual := isSystem("kube-system")
	if actual != true {
		t.Errorf("isSystem(`kube-system`)=%v, want false", actual)
	}
}

func Test_isSystem_default(t *testing.T) {
	actual := isSystem("default")
	if actual != false {
		t.Errorf("isSystem(`default`)=%v, want false", actual)
	}
}
