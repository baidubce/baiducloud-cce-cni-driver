package podmutating

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestMutatingPodHandler_generatePatches(t *testing.T) {
	tests := []struct {
		name        string
		mutatedPod  *corev1.Pod
		wantPatches []PatchOperation
		wantErr     bool
	}{
		{
			name: "empty pod",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{},
			},
			wantPatches: nil,
			wantErr:     false,
		},
		{
			name: "pod with no modifications",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{},
								Limits:   corev1.ResourceList{},
							},
						},
					},
				},
			},
			wantPatches: nil,
			wantErr:     false,
		},
		{
			name: "pod with container resources - requests only",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cce.baidubce.com/ip": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			wantPatches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/containers/0/resources",
					Value: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cce.baidubce.com/ip": resource.MustParse("1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with container resources - limits only",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cce.baidubce.com/eni": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			wantPatches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/containers/0/resources",
					Value: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cce.baidubce.com/eni": resource.MustParse("1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with affinity",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main"}},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "kubernetes.io/arch",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"amd64"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPatches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/affinity",
					Value: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "kubernetes.io/arch",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"amd64"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with topology spread constraints",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main"}},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				},
			},
			wantPatches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/topologySpreadConstraints",
					Value: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with all modifications",
			mutatedPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cce.baidubce.com/ip": resource.MustParse("1"),
								},
								Limits: corev1.ResourceList{
									"cce.baidubce.com/ip": resource.MustParse("1"),
								},
							},
						},
					},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "zone",
							WhenUnsatisfiable: corev1.DoNotSchedule,
						},
					},
				},
			},
			wantPatches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/containers/0/resources",
					Value: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cce.baidubce.com/ip": resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							"cce.baidubce.com/ip": resource.MustParse("1"),
						},
					},
				},
				{
					Op:   "add",
					Path: "/spec/affinity",
					Value: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
				{
					Op:   "add",
					Path: "/spec/topologySpreadConstraints",
					Value: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "zone",
							WhenUnsatisfiable: corev1.DoNotSchedule,
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MutatingPodHandler{}
			gotPatches, err := h.generatePatches(tt.mutatedPod)
			if (err != nil) != tt.wantErr {
				t.Errorf("generatePatches() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Handle comparison between nil and empty slice
			if (gotPatches == nil && tt.wantPatches != nil && len(tt.wantPatches) > 0) ||
				(gotPatches != nil && tt.wantPatches == nil && len(gotPatches) > 0) ||
				(!reflect.DeepEqual(gotPatches, tt.wantPatches) && !(len(gotPatches) == 0 && len(tt.wantPatches) == 0)) {
				t.Errorf("generatePatches() gotPatches = %v, want %v", gotPatches, tt.wantPatches)
			}
		})
	}
}

func TestMutatingPodHandler_createPatchResponse(t *testing.T) {
	tests := []struct {
		name    string
		patches []PatchOperation
		wantErr bool
		checkFn func(resp admission.Response) bool
	}{
		{
			name:    "empty patches",
			patches: []PatchOperation{},
			wantErr: false,
			checkFn: func(resp admission.Response) bool {
				return resp.Allowed && resp.Patch == nil
			},
		},
		{
			name: "valid patches",
			patches: []PatchOperation{
				{
					Op:    "add",
					Path:  "/spec/containers/0/resources",
					Value: corev1.ResourceRequirements{},
				},
			},
			wantErr: false,
			checkFn: func(resp admission.Response) bool {
				if !resp.Allowed {
					return false
				}

				var patches []PatchOperation
				if err := json.Unmarshal(resp.Patch, &patches); err != nil {
					return false
				}

				return len(patches) == 1 &&
					patches[0].Op == "add" &&
					patches[0].Path == "/spec/containers/0/resources" &&
					*resp.PatchType == admissionv1.PatchTypeJSONPatch
			},
		},
		{
			name: "patches with complex value",
			patches: []PatchOperation{
				{
					Op:   "add",
					Path: "/spec/affinity",
					Value: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "zone",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"zone-a", "zone-b"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			checkFn: func(resp admission.Response) bool {
				if !resp.Allowed {
					return false
				}

				if resp.Patch == nil || resp.PatchType == nil {
					return false
				}

				// Verify patch can be serialized correctly
				var patches []PatchOperation
				if err := json.Unmarshal(resp.Patch, &patches); err != nil {
					return false
				}

				return len(patches) == 1 &&
					patches[0].Op == "add" &&
					patches[0].Path == "/spec/affinity" &&
					*resp.PatchType == admissionv1.PatchTypeJSONPatch
			},
		},
		{
			name: "patches with non-serializable value (func)",
			patches: []PatchOperation{
				{
					Op:    "add",
					Path:  "/spec/test",
					Value: func() {}, // Function cannot be JSON serialized
				},
			},
			wantErr: true,
			checkFn: func(resp admission.Response) bool {
				// Should return error
				return !resp.Allowed && resp.Result != nil && resp.Result.Code == http.StatusInternalServerError
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MutatingPodHandler{}
			resp := h.createPatchResponse(tt.patches)

			if tt.checkFn != nil && !tt.checkFn(resp) {
				t.Errorf("createPatchResponse() response validation failed")
			}

			if resp.Result != nil && resp.Result.Code == http.StatusInternalServerError {
				if !tt.wantErr {
					t.Errorf("createPatchResponse() unexpected error response")
				}
			} else if tt.wantErr {
				t.Errorf("createPatchResponse() expected error but got success")
			}
		})
	}
}

// TestMutatingPodHandler_Handle_DecodeOnly only test decode logic, avoid business logic dependencies
func TestMutatingPodHandler_Handle_DecodeOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	decoder, _ := admission.NewDecoder(scheme)

	t.Run("decode error", func(t *testing.T) {
		h := &MutatingPodHandler{
			Decoder: decoder,
		}

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UID:       "test-uid",
				Operation: admissionv1.Create,
				Object: runtime.RawExtension{
					Raw: []byte("invalid json"),
				},
			},
		}

		resp := h.Handle(context.Background(), req)

		if resp.Allowed {
			t.Errorf("Handle() should return error for invalid JSON")
		}
	})
}

// TestMutatingPodHandler_Integration integration test, simulate pod modified by business logic
func TestMutatingPodHandler_Integration(t *testing.T) {
	h := &MutatingPodHandler{}

	t.Run("pod modified by business logic", func(t *testing.T) {
		// Simulate pod already modified by business logic
		mutatedPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "sidecar",
						Image: "sidecar:latest",
						// Fields here should be protected and not lost in the patch
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"cce.baidubce.com/ip": resource.MustParse("1"),
							},
							Limits: corev1.ResourceList{
								"cce.baidubce.com/ip": resource.MustParse("1"),
							},
						},
					},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "zone",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"zone-a"},
										},
									},
								},
							},
						},
					},
				},
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
					},
				},
			},
		}

		patches, err := h.generatePatches(mutatedPod)
		if err != nil {
			t.Fatalf("generatePatches() failed: %v", err)
		}

		// Should generate 3 patches
		if len(patches) != 3 {
			t.Errorf("generatePatches() should generate 3 patches, got %d", len(patches))
		}

		// Verify patch paths
		pathCount := make(map[string]int)
		for _, patch := range patches {
			pathCount[patch.Path]++
		}

		expectedPaths := map[string]int{
			"/spec/containers/0/resources":    1,
			"/spec/affinity":                  1,
			"/spec/topologySpreadConstraints": 1,
		}

		for path, expectedCount := range expectedPaths {
			if pathCount[path] != expectedCount {
				t.Errorf("Expected %d patches for path %s, got %d", expectedCount, path, pathCount[path])
			}
		}

		// Test createPatchResponse
		resp := h.createPatchResponse(patches)
		if !resp.Allowed {
			t.Errorf("createPatchResponse() should allow the request")
		}

		if resp.Patch == nil {
			t.Errorf("createPatchResponse() should include patch data")
		}

		// Verify patch type
		if resp.PatchType == nil || *resp.PatchType != admissionv1.PatchTypeJSONPatch {
			t.Errorf("createPatchResponse() should set patch type to JSONPatch")
		}

		// Verify patch content can be correctly deserialized
		var deserializedPatches []PatchOperation
		if err := json.Unmarshal(resp.Patch, &deserializedPatches); err != nil {
			t.Errorf("Patch should be valid JSON: %v", err)
		}

		if len(deserializedPatches) != len(patches) {
			t.Errorf("Deserialized patches count mismatch")
		}
	})
}

// TestPatchOperation_JSONSerialization test PatchOperation JSON serialization
func TestPatchOperation_JSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		patch    PatchOperation
		wantJSON string
	}{
		{
			name: "add operation",
			patch: PatchOperation{
				Op:    "add",
				Path:  "/spec/containers/0/resources",
				Value: map[string]interface{}{"requests": map[string]string{"cpu": "100m"}},
			},
			wantJSON: `{"op":"add","path":"/spec/containers/0/resources","value":{"requests":{"cpu":"100m"}}}`,
		},
		{
			name: "add operation without value",
			patch: PatchOperation{
				Op:   "remove",
				Path: "/spec/containers/0/resources",
			},
			wantJSON: `{"op":"remove","path":"/spec/containers/0/resources"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.patch)
			if err != nil {
				t.Errorf("JSON marshal failed: %v", err)
				return
			}

			if string(jsonBytes) != tt.wantJSON {
				t.Errorf("JSON serialization = %s, want %s", string(jsonBytes), tt.wantJSON)
			}
		})
	}
}

// TestMutatingPodHandler_InjectDecoder test InjectDecoder method
func TestMutatingPodHandler_InjectDecoder(t *testing.T) {
	h := &MutatingPodHandler{}
	scheme := runtime.NewScheme()
	decoder, _ := admission.NewDecoder(scheme)

	err := h.InjectDecoder(decoder)
	if err != nil {
		t.Errorf("InjectDecoder() failed: %v", err)
	}

	if h.Decoder != decoder {
		t.Errorf("Decoder not set correctly")
	}
}
