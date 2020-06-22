package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

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

func Exec(addr, certPath, keyPath string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/labels/owner", addOwnerLabel)
	server := &http.Server{
		Addr:    addr,
		Handler: &logger{mux},
	}
	log.Println("binding TLS listener on", server.Addr)
	log.Fatalln(server.ListenAndServeTLS(certPath, keyPath))
}

func main() {
	Exec(DefaultAddr, DefaultCertPath, DefaultKeyPath)
}

var dec = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
var podResource = metav1.GroupVersionResource{Version: "v1", Resource: "resourcePods"}
var reviewResource = metav1.GroupVersionResource{Version: "v1", Resource: "AdmissionReview"}

func closer(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Printf("error closing body err=%v\n", err)
	}
}

func addOwnerLabel(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if r.Method != http.MethodPost {
		http.Error(w, "only POST permitted", http.StatusMethodNotAllowed)
		return
	}
	defer closer(r.Body)

	if contentType != ApplicationJson {
		http.Error(w, "invalid content-type", http.StatusBadRequest)
		return
	}

	var review v1.AdmissionReview
	err := json.NewDecoder(r.Body).Decode(&review)
	if err != nil {
		http.Error(w, "error reading response body", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "nil admission request", http.StatusBadRequest)
		return
	}

	if isSystem(review.Request.Namespace) {
		http.Error(w, "will not modify resource in kube-* namespace", http.StatusForbidden)
		return
	}

	if review.Request.Resource != podResource {
		http.Error(w, "resource not a v1.Pod", http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	err = json.Unmarshal(review.Request.Object.Raw, &pod)
	if err != nil {
		http.Error(w, "unable to unmarshal kubernetes v1.Pod", http.StatusBadRequest)
		return
	}

	_, ok := pod.ObjectMeta.Labels["owner"]
	if ok {
		http.Error(w, "pod has owner", http.StatusForbidden)
		return
	}

	op := addOp("/metadata/labels/owner", "nathan.fisher")
	ops, err := json.Marshal([]operation{op})
	if err != nil {
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
			Patch:     ops,
		},
	}

	w.Header().Set("Content-Type", ApplicationJson)
	enc := json.NewEncoder(w)
	err = enc.Encode(&resp)
	if err != nil {
		http.Error(w, "unable to encode response json", http.StatusInternalServerError)
		return
	}
}

type operation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func addOp(path, value string) operation {
	return operation{
		Op:    "add",
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
}

func (l *logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wc := &responseCode{w, http.StatusOK}
	l.Handler.ServeHTTP(wc, r)
	log.Printf("status=%d method=%s path=%s\n", wc.code, r.Method, r.URL.Path)
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
