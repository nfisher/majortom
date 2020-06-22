package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	r := post("")
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

var resourcePods = metav1.GroupVersionResource{Version: "v1", Resource: "resourcePods"}

func Test_post(t *testing.T) {
	cases := map[string]struct{
		reqBody interface{}
		code int
		message string
	}{
		"empty body": {"", http.StatusBadRequest, "error reading response body" },
		"nil review request": {&v1.AdmissionReview{}, http.StatusBadRequest, "nil admission request"},
		"system namespace": {&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "kube-system"}}, http.StatusForbidden, "will not modify resource in kube-* namespace"},
		"deployments resource": {&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: metav1.GroupVersionResource{Version: "v1", Resource: "deployments"}}}, http.StatusBadRequest, "resource not a v1.Pod"},
		"empty pod payload": {&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods}}, http.StatusBadRequest, "unable to unmarshal kubernetes v1.Pod"},
		"pod with owner": {&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods, Object: podWithOwnerLabel()}}, http.StatusForbidden, "pod has owner"},
		"happy path":  {&v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods, Object: tidePod()}}, http.StatusOK, `{"kind":"AdmissionReview","apiVersion":"admission.k8s.io/v1"`},
	}

	for n, tc := range cases {
		tc := tc
		t.Run(n, func(t *testing.T) {
			r := post(tc.reqBody)
			w := httptest.NewRecorder()
			addOwnerLabel(w, r)
			if w.Code != tc.code {
				t.Errorf("w.Code=%v, want %v", w.Code, tc.code)
			}
			if !strings.HasPrefix(w.Body.String(), tc.message) {
				t.Errorf("response starts with <%v>, want <%v>", w.Body.String(), tc.message)
			}
		})
	}
}

func podWithOwnerLabel() runtime.RawExtension {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"owner": "betty.boop",
			},
		},
	}
	raw, _ := json.Marshal(&pod)

	return runtime.RawExtension{Raw: raw}
}

func tidePod() runtime.RawExtension {
	pod := &corev1.Pod{}
	raw, _ := json.Marshal(&pod)
	return runtime.RawExtension{Raw: raw}
}

func post(v interface{}) *http.Request {
	b, _ := json.Marshal(v)
	buf := bytes.NewBuffer(b)
	r, _ := http.NewRequest(http.MethodPost, "/", buf)
	r.Header.Set("Content-Type", ApplicationJson)
	return r
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
