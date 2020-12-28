package v1alpha1

import "k8s.io/apimachinery/pkg/runtime"

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_RecorderSpec(obj *RecorderSpec) {
	if obj.Action == "" {
		obj.Action = RecorderActionNone
	}
}
