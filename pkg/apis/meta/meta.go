package meta

import (
	"fmt"

	"github.com/weaveworks/libgitops/pkg/runtime"
	"github.com/weaveworks/libgitops/pkg/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Validatable interface {
	Validate() error
}

type Singleton interface {
	IsSingleton() bool
}

type Namespaced interface {
	IsNamespaced() bool
}

func ValidateIfPossible(obj interface{}) error {
	v, ok := obj.(Validatable)
	if !ok {
		return nil
	}
	return v.Validate()
}

func IsSingleton(obj interface{}) bool {
	s, ok := obj.(Singleton)
	return ok && s.IsSingleton()
}

func IsNamespaced(obj interface{}) bool {
	ns, ok := obj.(Namespaced)
	return ok && ns.IsNamespaced()
}

const SingletonIdentifier = "singleton"

func SingletonKey(kind storage.KindKey) storage.ObjectKey {
	return storage.NewObjectKey(kind, runtime.NewIdentifier(SingletonIdentifier))
}

type Metav1NameIdentifierFactory struct{}

func (id Metav1NameIdentifierFactory) Identify(o interface{}) (runtime.Identifyable, bool) {
	if IsSingleton(o) {
		return runtime.NewIdentifier(SingletonIdentifier), true
	}
	switch obj := o.(type) {
	case metav1.Object:
		// If the object opted-out of namespacing explicitely, only use the name
		if ns, ok := o.(Namespaced); ok && !ns.IsNamespaced() {
			return runtime.NewIdentifier(obj.GetName()), true
		}
		// Otherwise continue "as normal"
		// TODO: Add in "default" here automatically?
		if len(obj.GetNamespace()) == 0 || len(obj.GetName()) == 0 {
			return nil, false
		}
		return runtime.NewIdentifier(fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())), true
	}
	return nil, false
}