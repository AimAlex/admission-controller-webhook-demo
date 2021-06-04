/*
Copyright (c) 2019 StackRox Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

var (
	podResource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

// applySecurityDefaults implements the logic of our example admission controller webhook. For every pod that is created
// (outside of Kubernetes namespaces), it first checks if `runAsNonRoot` is set. If it is not, it is set to a default
// value of `false`. Furthermore, if `runAsUser` is not set (and `runAsNonRoot` was not initially set), it defaults
// `runAsUser` to a value of 1234.
//
// To demonstrate how requests can be rejected, this webhook further validates that the `runAsNonRoot` setting does
// not conflict with the `runAsUser` setting - i.e., if the former is set to `true`, the latter must not be `0`.
// Note that we combine both the setting of defaults and the check for potential conflicts in one webhook; ideally,
// the latter would be performed in a validating webhook admission controller.
func applySecurityDefaults(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	// This handler should only get called on Pod objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	if req.Resource != podResource {
		log.Printf("expect resource to be %s", podResource)
		return nil, nil
	}

	// Parse the Pod object.
	raw := req.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("could not deserialize pod object: %v", err)
	}

	// check if pod is application
	if pod.Annotations["aic.4paradigm.com/app"] != "true" {
		return nil, nil
	}

	// get computeunitMap in annotation
	var containerComputeunitMap = map[string]string{}
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, "aic.4paradigm.com/computeunit/") {
			containerName := strings.TrimPrefix(k, "aic.4paradigm.com/computeunit/")
			containerComputeunitMap[containerName] = v
		}
	}
	// Create patch operations to apply sensible defaults, if those options are not set explicitly.
	var patches []patchOperation
	var computeunitList = []string{}
	//var volumes = []interface{}{}
	//var volumesExists = map[string]bool{}
	for _, pod := range pod.Spec.Containers {
		if computeunit, ok := containerComputeunitMap[pod.Name]; ok {
			// TODO: get computeunit from billing server
			computeunitList = append(computeunitList, computeunit)
			// TODO: check volumes
			// TODO: patch operation
			delete(containerComputeunitMap, pod.Name)
		}
	}

	if len(containerComputeunitMap) != 0 {
		return nil, errors.New("unexpected computeunit")
	}
	//if computeunit == "single-core" {
	//	for item := range pod.Spec.Containers {
	//		itemString := strconv.FormatInt(int64(item), 10)
	//		resources := Resources{
	//			Limits: map[string]string{"cpu": "1", "memory": "1Gi"},
	//			Requests: map[string]string{"cpu": "1", "memory": "1Gi"}}
	//		patches = append(patches, patchOperation{
	//			Op: "add",
	//			Path: "/spec/containers/" + itemString + "/resources",
	//			Value: resources,
	//		})
			//patches = append(patches, patchOperation{
			//	Op: "add",
			//	Path: "/spec/containers/" + itemString + "/resources/limits/memory",
			//	Value: "1Gi",
			//})
			//patches = append(patches, patchOperation{
			//	Op: "add",
			//	Path: "/spec/containers/" + itemString + "/resources/requests/cpu",
			//	Value: "1",
			//})
			//patches = append(patches, patchOperation{
			//	Op: "add",
			//	Path: "/spec/containers/" + itemString + "/resources/requests/memory",
			//	Value: "1Gi",

	return patches, nil
}

func main() {
	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)

	mux := http.NewServeMux()
	mux.Handle("/mutate", admitFuncHandler(applySecurityDefaults))
	server := &http.Server{
		// We listen on port 8443 such that we do not need root privileges or extra capabilities for this server.
		// The Service object will take care of mapping this port to the HTTPS port 443.
		Addr:    ":8443",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}
