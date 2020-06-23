package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultAddr     = ":8443"
	DefaultCertPath = "/run/secrets/tls/tls.crt"
	DefaultKeyPath  = "/run/secrets/tls/tls.key"
	ApplicationJson = `application/json`
)

var (
	// Revision is the git revision of the binary
	Revision = "dev"
)

const LogFlags = log.LstdFlags | log.LUTC | log.Lshortfile | log.Lmsgprefix

func Exec(addr, certPath, keyPath string) {
	prefix := fmt.Sprintf("rev=%s ", Revision)
	log.SetFlags(LogFlags)
	log.SetPrefix(prefix)
	lg := log.New(os.Stderr, prefix, LogFlags)
	mux := http.NewServeMux()
	mux.HandleFunc("/labels/owner", bind(podPatch, VarPatch("NODEIP", "status.hostIP")))
	server := &http.Server{
		Addr: addr,
		Handler: &logger{
			Handler: mux,
			Logger:  lg,
		},
	}
	lg.Printf("status=binding addr=%s\n", server.Addr)
	lg.Fatalln(server.ListenAndServeTLS(certPath, keyPath))
}

func main() {
	Exec(DefaultAddr, DefaultCertPath, DefaultKeyPath)
}

var podResource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}

func closer(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Printf("error closing body err=%v\n", err)
	}
}

var ErrPodHasOwnerLabel = fmt.Errorf("pod has owner")

func AddOwner(pod *corev1.Pod) ([]operation, error) {
	_, ok := pod.ObjectMeta.Labels["owner"]
	if ok {
		return nil, ErrPodHasOwnerLabel
	}
	op := addOp("/metadata/labels/owner", "nathan.fisher")
	return []operation{op}, nil
}

func varReplace(cid, eid int, name, value string) operation {
	path := fmt.Sprintf("/spec/containers/%d/env/%d", cid, eid)
	pathValue := map[string]interface{}{
		"name": name,
		"valueFrom": map[string]interface{}{
			"fieldRef": map[string]interface{}{
				"fieldPath": value,
			},
		},
	}
	return replaceOp(path, pathValue)
}

func varAdd(cid, eid int, name, value string) operation {
	if eid == 0 {
		path := fmt.Sprintf("/spec/containers/%d/env", cid)
		pathValue := []map[string]interface{}{
			{
				"name": name,
				"valueFrom": map[string]interface{}{
					"fieldRef": map[string]interface{}{
						"fieldPath": value,
					},
				},
			},
		}
		return addOp(path, pathValue)
	}
	path := fmt.Sprintf("/spec/containers/%d/env/%d", cid, eid)
	pathValue := map[string]interface{}{
		"name": name,
		"valueFrom": map[string]interface{}{
			"fieldRef": map[string]interface{}{
				"fieldPath": value,
			},
		},
	}
	return addOp(path, pathValue)
}

func VarPatch(name, value string) PodPatchable {
	return func(pod *corev1.Pod) ([]operation, error) {
		var ops []operation
		for i := range pod.Spec.Containers {
			container := pod.Spec.Containers[i]
			var op operation
			var wasFound = false
			for j := range container.Env {
				env := container.Env[j]
				if env.Name == name {
					wasFound = true
					op = varReplace(i, j, name, value)
					break
				}
			}
			if !wasFound {
				op = varAdd(i, len(container.Env), name, value)
			}
			ops = append(ops, op)
		}
		return ops, nil
	}
}

type PodPatchable func(*corev1.Pod) ([]operation, error)

func bind(handler func(http.ResponseWriter, *http.Request, PodPatchable), patchable PodPatchable) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, patchable)
	}
}

func podPatch(w http.ResponseWriter, r *http.Request, apply PodPatchable) {
	contentType := r.Header.Get("Content-Type")
	if r.Method != http.MethodPost {
		log.Printf("status=failed path=%s err='invalid request method %s'", r.URL.Path, r.Method)
		http.Error(w, "only POST permitted", http.StatusMethodNotAllowed)
		return
	}
	defer closer(r.Body)

	if contentType != ApplicationJson {
		log.Printf("status=failed path=%s err='invalid content-type %s'", r.URL.Path, contentType)
		http.Error(w, "invalid content-type", http.StatusBadRequest)
		return
	}

	var review v1.AdmissionReview
	err := json.NewDecoder(r.Body).Decode(&review)
	if err != nil {
		log.Printf("status=failed path=%s err='admission review unmarshal: %v'", r.URL.Path, err)
		http.Error(w, "error reading response body", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		log.Printf("status=failed path=%s err='request was nil'", r.URL.Path)
		http.Error(w, "nil admission request", http.StatusBadRequest)
		return
	}

	if isSystem(review.Request.Namespace) {
		log.Printf("status=ignored path=%s err='system namespace %s'", r.URL.Path, review.Request.Namespace)
		http.Error(w, "will not modify resource in kube-* namespace", http.StatusForbidden)
		return
	}

	if review.Request.Resource != podResource {
		log.Printf("status=failed path=%s err='unexpected resource got %#v, want %#v'", r.URL.Path, review.Request.Resource, podResource)
		http.Error(w, "resource not a v1.Pod", http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	err = json.Unmarshal(review.Request.Object.Raw, &pod)
	if err != nil {
		log.Printf("status=failed path=%s err='pod unmarshal: %v'", r.URL.Path, err)
		http.Error(w, "unable to unmarshal kubernetes v1.Pod", http.StatusBadRequest)
		return
	}

	ops, err := apply(&pod)
	if err != nil {
		log.Printf("status=failed path=%s err='apply: %v'", r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	patch, err := json.Marshal(ops)
	if err != nil {
		log.Printf("status=failed path=%s err='ops marshal: %v'", r.URL.Path, err)
		http.Error(w, "unable to marshal operation json", http.StatusInternalServerError)
		return
	}

	pt := v1.PatchTypeJSONPatch
	resp := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
		Response: &v1.AdmissionResponse{
			UID:       review.Request.UID,
			Allowed:   true,
			PatchType: &pt,
			Patch:     patch,
		},
	}

	w.Header().Set("Content-Type", ApplicationJson)
	enc := json.NewEncoder(w)
	err = enc.Encode(&resp)
	if err != nil {
		log.Printf("status=failed path=%s err='admission review marshal: %v'", r.URL.Path, err)
		http.Error(w, "unable to encode response json", http.StatusInternalServerError)
		return
	}
}

type operation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func addOp(path string, value interface{}) operation {
	return operation{
		Op:    "add",
		Path:  path,
		Value: value,
	}
}

func replaceOp(path string, value interface{}) operation {
	return operation{
		Op:    "replace",
		Path:  path,
		Value: value,
	}
}

type responseCode struct {
	http.ResponseWriter
	code int
}

func (w *responseCode) WriteHeader(statusCode int) {
	w.code = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

type logger struct {
	Handler http.Handler
	Logger  *log.Logger
}

func (l *logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wc := &responseCode{w, http.StatusOK}
	l.Handler.ServeHTTP(wc, r)
	l.Logger.Printf("status=%d method=%s path=%s\n", wc.code, r.Method, r.URL.Path)
}

func isSystem(namespace string) bool {
	if namespace == metav1.NamespacePublic {
		return true
	}
	if namespace == metav1.NamespaceSystem {
		return true
	}
	return false
}
