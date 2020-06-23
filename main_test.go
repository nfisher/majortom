package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	log.SetOutput(ioutil.Discard)
}

func Test_get_should_not_be_allowed_method(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	podPatch(w, r, AddOwner)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("w.Code=%v, want StatusMethodNotAllowed", w.Code)
	}
}

func Test_non_json_content_type_should_be_invalid(t *testing.T) {
	r := post("")
	r.Header.Set("Content-Type", "text/html")
	w := httptest.NewRecorder()
	podPatch(w, r, AddOwner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("w.Code=%v, want StatusBadRequest", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "invalid content-type") {
		t.Errorf("w.Body starts with <%v>, want `invalid content-type`", w.Body.String())
	}
}

var resourcePods = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}

func Test_post(t *testing.T) {
	cases := map[string]struct {
		code    int
		reqBody interface{}
		message string
	}{
		"empty body":           {http.StatusBadRequest, "", "error reading response body"},
		"nil review request":   {http.StatusBadRequest, &v1.AdmissionReview{}, "nil admission request"},
		"system namespace":     {http.StatusForbidden, &v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "kube-system"}}, "will not modify resource in kube-* namespace"},
		"deployments resource": {http.StatusBadRequest, &v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: metav1.GroupVersionResource{Version: "v1", Resource: "deployments"}}}, "resource not a v1.Pod"},
		"empty pod payload":    {http.StatusBadRequest, &v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods}}, "unable to unmarshal kubernetes v1.Pod"},
		"pod with owner":       {http.StatusForbidden, &v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods, Object: podWithOwnerLabel()}}, "pod has owner"},
		"happy path":           {http.StatusOK, &v1.AdmissionReview{Request: &v1.AdmissionRequest{Namespace: "default", Resource: resourcePods, Object: tidePod()}}, `{"kind":"AdmissionReview","apiVersion":"admission.k8s.io/v1"`},
	}

	for n, tc := range cases {
		tc := tc
		h := bind(podPatch, AddOwner)
		t.Run(n, func(t *testing.T) {
			r := post(tc.reqBody)
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != tc.code {
				t.Errorf("w.Code=%v, want %v", w.Code, tc.code)
			}
			if !strings.HasPrefix(w.Body.String(), tc.message) {
				t.Errorf("response starts with <%v>, want <%v>", w.Body.String(), tc.message)
			}
		})
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

func Test_logger_handler(t *testing.T) {
	var buf bytes.Buffer
	lg := log.New(&buf, "", 0)
	mux := http.NewServeMux()
	h := logger{mux, lg}
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	actual := buf.String()
	expected := "status=404 method=GET path=/\n"
	if actual != expected {
		t.Errorf("log=`%s`, want `%s`", actual, expected)
	}
}

func Test_patch_env_var_to_single_container(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Image: "nginx:latest"}}},
	}
	ops, err := VarPatch("NODEIP", "status.hostIP")(&pod)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
	if len(ops) != 1 {
		t.Errorf("len(ops)=%v, want 1", len(ops))
	}
	expected := operation{
		Op:   "add",
		Path: "/spec/containers/0",
		Value: map[string]interface{}{
			"env": []map[string]interface{}{
				{
					"name": "NODEIP",
					"valueFrom": map[string]interface{}{
						"fieldRef": map[string]interface{}{
							"fieldPath": "status.hostIP",
						},
					},
				},
			},
		},
	}
	if !cmp.Equal(ops[0], expected) {
		t.Errorf("ops mismatch (+want -got)\n%s", cmp.Diff(ops[0], expected))
	}
}

func Test_patch_with_add(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "nginx:latest"}}},
	}
	ops := []operation{varAdd(0, "NODEIP", "status.nodeIP")}
	patchBytes, _ := json.Marshal(ops)
	podBytes, _ := json.Marshal(&pod)
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Errorf("DecodePatch err=%v, want nil", err)
	}
	patched, err := patch.Apply(podBytes)
	if err != nil {
		t.Errorf("patch.Apply err=%v, want nil", err)
	}
	var podWithPatch corev1.Pod
	err = json.Unmarshal(patched, &podWithPatch)
	if err != nil {
		t.Errorf("json.Unmarshal err=%v, want nil", err)
	}
	if len(podWithPatch.Spec.Containers[0].Env) != 1 {
		t.Errorf("len(container[0].env)=%d, want 1", len(podWithPatch.Spec.Containers[0].Env))
	}
}

func Test_patch_with_replace(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "nginx:latest", Env: []corev1.EnvVar{{Name: "NODEIP", Value: "localhost"}}}}},
	}
	ops := []operation{varReplace(0, 0, "NODEIP", "status.nodeIP")}
	patchBytes, _ := json.Marshal(ops)
	podBytes, _ := json.Marshal(&pod)
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Errorf("DecodePatch err=%v, want nil", err)
	}
	patched, err := patch.Apply(podBytes)
	if err != nil {
		t.Errorf("patch.Apply err=%v, want nil", err)
	}

	var podWithPatch corev1.Pod
	err = json.Unmarshal(patched, &podWithPatch)
	if err != nil {
		t.Errorf("json.Unmarshal err=%v, want nil", err)
	}
	if podWithPatch.Spec.Containers[0].Env[0].Value != "" {
		t.Errorf("container[0].env[0].value=%s, want ``", podWithPatch.Spec.Containers[0].Env[0].Value)
	}
	if podWithPatch.Spec.Containers[0].Env[0].ValueFrom == nil {
		t.Error("container[0].env[0].valueFrom=nil, want <map>")
	}
}

func Test_patch_env_var_to_multiple_containers(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Image: "nginx:latest", Env: []corev1.EnvVar{{Name: "REMOTE", Value: "junctionbox.ca"}, {Name: "NODEIP", Value: "localhost"}}},
			{Image: "istio:latest", Env: []corev1.EnvVar{{Name: "REMOTE", Value: "junctionbox.ca"}}},
		}},
	}
	ops, err := VarPatch("NODEIP", "status.hostIP")(&pod)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
	if len(ops) != 2 {
		t.Errorf("len(ops)=%v, want 2", len(ops))
	}
	expected := []operation{
		{
			Op:   "replace",
			Path: "/spec/containers/0/env/1",
			Value: map[string]interface{}{
				"name": "NODEIP",
				"valueFrom": map[string]interface{}{
					"fieldRef": map[string]interface{}{
						"fieldPath": "status.hostIP",
					},
				},
			},
		},
		{
			Op:   "add",
			Path: "/spec/containers/1",
			Value: map[string]interface{}{
				"env": []map[string]interface{}{
					{
						"name": "NODEIP",
						"valueFrom": map[string]interface{}{
							"fieldRef": map[string]interface{}{
								"fieldPath": "status.hostIP",
							},
						},
					},
				},
			},
		},
	}
	if !cmp.Equal(ops, expected) {
		t.Errorf("ops mismatch (+want -got)\n%s", cmp.Diff(ops, expected))
	}
}

func Test_patch_update_env_var_in_single_container(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "nginx:latest", Env: []corev1.EnvVar{{Name: "REMOTE", Value: "junctionbox.ca"}, {Name: "NODEIP", Value: "localhost"}}}}},
	}
	ops, err := VarPatch("NODEIP", "status.hostIP")(&pod)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
	if len(ops) != 1 {
		t.Errorf("len(ops)=%v, want 1", len(ops))
	}
	expected := operation{
		Op:   "replace",
		Path: "/spec/containers/0/env/1",
		Value: map[string]interface{}{
			"name": "NODEIP",
			"valueFrom": map[string]interface{}{
				"fieldRef": map[string]interface{}{
					"fieldPath": "status.hostIP",
				},
			},
		},
	}
	if !cmp.Equal(ops[0], expected) {
		t.Errorf("ops mismatch (+want -got)\n%s", cmp.Diff(ops[0], expected))
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
