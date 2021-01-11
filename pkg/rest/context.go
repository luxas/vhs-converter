package rest

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

type StorageContext interface {
	echo.Context
	// Warn headers
	Warn(err error)
	Warnf(format string, args ...interface{})
	// Access to the Storage
	Storage() storage.Storage
}

type ResourceContext interface {
	StorageContext

	Resource() GroupVersionKindResource
	// Conditionally defaults the answer when returning
	JSONIndent(code int, obj runtime.Object) error
	KindKey() storage.KindKey
	// Applies TypeMeta info to the Object or List
	NewObject() (storage.Object, error)
	// TODO: Remove this in favor for typed Lists, or
	// alternatively, implement a To/FromUnstructured defaulter in pkg/serializer
	ApplyTypeMetaToList(obj storage.ObjectList)
}

type NamespacedResourceContext interface {
	ResourceContext
	Namespace() string
}

type NamedResourceContext interface {
	ResourceContext
	Name() string
	NamespacedName() storage.NamespacedName

	NewNamedObject() (storage.Object, error)
	ApplyName(obj storage.Object)
}

var _ StorageContext = &storageContextImpl{}

type storageContextImpl struct {
	echo.Context
	s storage.Storage
}

func (cc *storageContextImpl) Warn(err error) {
	cc.Warnf(err.Error())
}

func (cc *storageContextImpl) Warnf(format string, args ...interface{}) {
	cc.Response().Header().Add("Warning", fmt.Sprintf(format, args...))
}

func (cc *storageContextImpl) Storage() storage.Storage {
	return cc.s
}

func storageContextMiddleware(s storage.Storage) func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &storageContextImpl{c, s}
			return next(cc)
		}
	}
}

var _ ResourceContext = &resourceContextImpl{}

type resourceContextImpl struct {
	StorageContext
	gvkr GroupVersionKindResource
}

func (cc *resourceContextImpl) Resource() GroupVersionKindResource {
	return cc.gvkr
}

func (cc *resourceContextImpl) JSONIndent(code int, obj runtime.Object) error {
	// Do automatic conditional defaulting of the returned response
	// TODO: Do defaulting before validation?
	if cc.ShouldDefault() {
		objs := []runtime.Object{obj} // default case
		if kmeta.IsListType(obj) {    // list case
			var err error
			// TODO: This only works with typed Lists
			objs, err = kmeta.ExtractList(obj)
			if err != nil {
				return err
			}
		}
		if err := cc.Storage().Serializer().Defaulter().Default(objs...); err != nil {
			return err
		}
	}

	cc.Response().WriteHeader(code)
	return cc.Storage().Serializer().
		Encoder(serializer.WithPrettyEncode(true)).
		EncodeForGroupVersion(serializer.NewJSONFrameWriter(cc.Response()), obj, cc.gvkr.GVK().GroupVersion())
}

func (cc *resourceContextImpl) KindKey() storage.KindKey {
	return cc.gvkr.GVK()
}

func (cc *resourceContextImpl) NewObject() (storage.Object, error) {
	return storage.NewObjectForGVK(cc.gvkr.GVK(), cc.Storage().Serializer().Scheme())
}

func (cc *resourceContextImpl) ApplyTypeMetaToObject(obj storage.Object) {
	obj.GetObjectKind().SetGroupVersionKind(cc.KindKey())
}
func (cc *resourceContextImpl) ApplyTypeMetaToList(obj storage.ObjectList) {
	gvk := cc.KindKey()
	obj.GetObjectKind().SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
}

func (cc *resourceContextImpl) ShouldDefault() bool {
	h := cc.Request().Header.Get("default")
	return len(h) == 0 || h == "true" // default to true for the empty header case
}

func resourceContextMiddleware(gvkr GroupVersionKindResource) func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &resourceContextImpl{c.(StorageContext), gvkr}
			return next(cc)
		}
	}
}

var _ NamespacedResourceContext = &namespacedResourceContextImpl{}

type namespacedResourceContextImpl struct {
	ResourceContext
	namespace string
}

func (cc *namespacedResourceContextImpl) Namespace() string {
	return cc.namespace
}

func namespacedResourceContextMiddleware() func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rc := c.(ResourceContext)
			namespace := rc.Param("namespace")
			if !rc.Resource().Namespaced {
				return newStatusErrorf(http.StatusInternalServerError, "NamespacedResourceContext middleware requires a Namespaced resource")
			}
			if namespace == "" {
				return newStatusErrorf(http.StatusBadRequest, "namespace parameter is mandatory")
			}
			cc := &namespacedResourceContextImpl{rc, namespace}
			return next(cc)
		}
	}
}

var _ NamedResourceContext = &namedResourceContextImpl{}

type namedResourceContextImpl struct {
	ResourceContext
	name string
}

func (cc *namedResourceContextImpl) Name() string {
	return cc.name
}

func (cc *namedResourceContextImpl) NamespacedName() storage.NamespacedName {
	n := storage.NamespacedName{
		Name: cc.name,
	}
	if ns, ok := cc.ResourceContext.(NamespacedResourceContext); ok {
		n.Namespace = ns.Namespace()
	}
	return n
}

func (cc *namedResourceContextImpl) NewNamedObject() (storage.Object, error) {
	obj, err := cc.NewObject()
	if err != nil {
		return nil, err
	}
	cc.ApplyName(obj)
	return obj, nil
}

func (cc *namedResourceContextImpl) ApplyName(obj storage.Object) {
	nsName := cc.NamespacedName()
	obj.SetName(nsName.Name)
	obj.SetNamespace(nsName.Namespace)
}

func namedResourceContextMiddleware() func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rc := c.(ResourceContext)
			// Set name conditionally
			name := ""
			if rc.Resource().Singleton {
				name = meta.SingletonSpecialName
			} else {
				name = rc.Param("name")
				if name == "" {
					return newStatusErrorf(http.StatusBadRequest, "name parameter is mandatory")
				}
			}
			cc := &namedResourceContextImpl{rc, name}
			return next(cc)
		}
	}
}
