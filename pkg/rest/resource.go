package rest

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/labstack/echo"
	"github.com/luxas/digitized/pkg/apis/digitized.luxaslabs.com/v1alpha1"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/weaveworks/libgitops/pkg/filter"
	"github.com/weaveworks/libgitops/pkg/runtime"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	kruntime "k8s.io/apimachinery/pkg/runtime"
)

const nameParamPath = "/:name/"

type (
	// ResourceHookHandler is a hook handler that operates in a resource context.
	ResourceHookHandler func(ResourceContext) error
	// NamedResourceHookHandler is a hook handler that operates in a named resource context.
	NamedResourceHookHandler func(NamedResourceContext) error
	// ObjectHookHandler is a hook handler that operates in resource context, where there
	// is JSON data in the body that has been is decoded. The hook executes before data is
	// saved to the storage though, so obj is a pointer that can be mutated.
	ObjectHookHandler func(rc ResourceContext, obj runtime.Object) error
)

func newResourceHandler(parent *echo.Group, gvkr GroupVersionKindResource) ResourceHandler {
	rh := &resourceHandlerImpl{nil, gvkr, nil, nil, nil}

	resourcePath := "/" + gvkr.Resource
	if gvkr.Namespaced {
		// List resources in all namespaces
		parent.GET(resourcePath+"/", rh.list, resourceContextMiddleware(gvkr))
		// Add the namespaced resource route
		rh.g = parent.Group("/namespaces/:namespace"+resourcePath, resourceContextMiddleware(gvkr), namespacedResourceContextMiddleware())
	} else {
		rh.g = parent.Group(resourcePath, resourceContextMiddleware(gvkr))
	}

	// Register the relevant routes
	rh.g.POST("/", rh.create)
	rh.g.PATCH("/", rh.patch)
	rh.g.PUT("/", rh.update)
	// Singletons don't have named routes, nor LIST
	if gvkr.Singleton {
		rh.g.GET("/", rh.getSingleton)
	} else {
		rh.g.GET("/", rh.list)
		rh.g.GET(nameParamPath, rh.get, namedResourceContextMiddleware())
		rh.g.DELETE(nameParamPath, rh.delete, namedResourceContextMiddleware())
	}
	return rh
}

type resourceHandlerImpl struct {
	g           *echo.Group
	gvkr        GroupVersionKindResource
	postHooks   []ObjectHookHandler
	putHooks    []ObjectHookHandler
	deleteHooks []NamedResourceHookHandler
}

func (rh *resourceHandlerImpl) Resource() GroupVersionKindResource {
	return rh.gvkr
}

func (rh *resourceHandlerImpl) RegisterSubResource(name string, fn ResourceHookHandler) {
	rh.g.PUT("/"+name+"/", func(c echo.Context) error {
		return fn(c.(ResourceContext))
	})
}

func (rh *resourceHandlerImpl) RegisterNamedSubResource(name string, fn NamedResourceHookHandler) {
	rh.g.PUT(nameParamPath+name+"/", func(c echo.Context) error {
		return fn(c.(NamedResourceContext))
	}, namedResourceContextMiddleware())
}

func (rh *resourceHandlerImpl) RegisterPOSTHook(handler ObjectHookHandler) {
	rh.postHooks = append(rh.postHooks, handler)
}

func (rh *resourceHandlerImpl) RegisterPUTHook(handler ObjectHookHandler) {
	rh.putHooks = append(rh.putHooks, handler)
}

func (rh *resourceHandlerImpl) RegisterDELETEHook(handler NamedResourceHookHandler) {
	rh.deleteHooks = append(rh.deleteHooks, handler)
}

func (rh *resourceHandlerImpl) validateBody(rc ResourceContext, obj runtime.Object) error {
	// Only validate ObjectMeta for non-singletons
	if !rh.gvkr.Singleton {
		if obj.GetName() == "" {
			return fmt.Errorf("name must be set")
		}
		// Validate namespace
		// TODO: Make namespace a tri-state (never namespace, default: use default ns, always set)?
		if meta.IsNamespaced(obj) {
			if obj.GetNamespace() == "" {
				return fmt.Errorf(".metadata.namespace must be set for namespaced object")
			}
		} else {
			if obj.GetNamespace() != "" {
				rc.Warnf("Non-namespaced resources must not have the .metadata.namespace field set. Pruning the field")
				obj.SetNamespace("")
			}
		}
	}
	// Enforce that the namespaces match
	if ns, ok := rc.(NamespacedResourceContext); ok && ns.Namespace() != obj.GetNamespace() {
		return fmt.Errorf("Object namespace in body: %q doesn't match namespace in path: %q", obj.GetNamespace(), ns.Namespace())
	}
	// Validate the object if possible
	return meta.ValidateIfPossible(obj)
}

func (rh *resourceHandlerImpl) returnFromStorage(rc ResourceContext, key storage.ObjectKey) error {
	obj, err := rc.Storage().Get(key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return newStatusError(http.StatusNotFound, storage.ErrNotFound)
		}
		return err
	}
	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) list(c echo.Context) error {
	rc := c.(ResourceContext)
	// Conditionally filter on namespace
	var filters []filter.ListOption
	if ns, ok := rc.(NamespacedResourceContext); ok {
		filters = append(filters, namespaceFilter{Namespace: ns.Namespace()})
	}
	list, err := rc.Storage().List(rc.KindKey(), filters...)
	if err != nil {
		return err
	}
	// Need to do dummy conversion
	runtimeList := make([]kruntime.Object, 0, len(list))
	for _, item := range list {
		runtimeList = append(runtimeList, item)
	}

	return rc.JSONIndent(http.StatusOK, &v1alpha1.List{Items: runtimeList})
}

func (rh *resourceHandlerImpl) decodeObject(rc ResourceContext) (runtime.Object, error) {
	// Decode the body and cast it to libgitops runtime.Object
	kobj, err := rc.Storage().Serializer().Decoder().Decode(serializer.NewJSONFrameReader(rc.Request().Body))
	if err != nil {
		return nil, newStatusError(http.StatusBadRequest, err)
	}
	obj, ok := kobj.(runtime.Object)
	if !ok {
		return nil, newStatusErrorf(http.StatusInternalServerError, "couldn't cast decoded object to runtime.Object")
	}

	// Validate the body
	if err := rh.validateBody(rc, obj); err != nil {
		return nil, newStatusError(http.StatusBadRequest, err)
	}
	return obj, nil
}

func (rh *resourceHandlerImpl) create(c echo.Context) error {
	rc := c.(ResourceContext)

	obj, err := rh.decodeObject(rc)
	if err != nil {
		return err
	}

	// Run all hooks in order
	for _, hook := range rh.postHooks {
		if err := hook(rc, obj); err != nil {
			return err
		}
	}

	// Write to storage
	if err := rc.Storage().Create(obj); err != nil {
		// Return BadRequest if the resource already exists
		if errors.Is(err, storage.ErrAlreadyExists) {
			return newStatusError(http.StatusBadRequest, storage.ErrAlreadyExists)
		}
		return newStatusError(http.StatusInternalServerError, err)
	}

	return rc.JSONIndent(http.StatusCreated, obj)
}

func (rh *resourceHandlerImpl) patch(c echo.Context) error {
	rc := c.(ResourceContext)

	// We need to read the patch twice, once for getting the ObjectKey info
	patch, err := ioutil.ReadAll(rc.Request().Body)
	if err != nil {
		return err
	}
	// rc.Request().Body.Close() // do we need this?

	// Decode the patch to a so-called "Partial Object" to extract the ObjectKey
	objs, err := storage.DecodePartialObjects(
		ioutil.NopCloser(bytes.NewReader(patch)),
		rc.Storage().Serializer().Scheme(),
		false, // guarantees that len(objs)==1 when err==nil
		nil,   // no default GVK although we "could", the caller should be specific
	)
	if err != nil {
		return newStatusErrorf(http.StatusBadRequest, "couldn't decode patch: %v", err)
	}
	// Wrap the decoded object in an extension that knows how to propagate namespaced and singleton
	// information. Now, extract the correct ObjectKey for this patch
	patchObj := &customPartialObject{objs[0], rh.gvkr.Namespaced, rh.gvkr.Singleton}
	key, err := rc.Storage().ObjectKeyFor(patchObj)
	if err != nil {
		return err
	}

	// TODO: Verify that GVKR & decoded object GVK match
	// TODO: Fix Storage.Patch to work with RawStorage & YAML
	// TODO: Use the same logic as PUT underneath, to make use of validateBody

	// Write the patch to the underlying storage
	// TODO: Make sure we validate the new payload here before applying
	if err := rc.Storage().Patch(key, patch); err != nil {
		return err
	}
	// After doing the patch, return the new object from storage
	return rh.returnFromStorage(rc, key)
}

func (rh *resourceHandlerImpl) update(c echo.Context) error {
	rc := c.(ResourceContext)

	obj, err := rh.decodeObject(rc)
	if err != nil {
		return err
	}

	// Run all hooks in order
	for _, hook := range rh.putHooks {
		if err := hook(rc, obj); err != nil {
			return err
		}
	}

	// Write to storage
	// TODO: storage.GVKForObject should check for obj==nil
	if err := rc.Storage().Update(obj); err != nil {
		return err
	}

	return rc.JSONIndent(http.StatusOK, obj)
}

func (rh *resourceHandlerImpl) get(c echo.Context) error {
	rc := c.(NamedResourceContext)
	return rh.returnFromStorage(rc, rc.ObjectKey())
}

func (rh *resourceHandlerImpl) getSingleton(c echo.Context) error {
	rc := c.(ResourceContext)
	// TODO: Maybe support namespaced singletons in the future
	return rh.returnFromStorage(rc, meta.SingletonKey(rc.KindKey()))
}

func (rh *resourceHandlerImpl) delete(c echo.Context) error {
	rc := c.(NamedResourceContext)

	// Run all hooks in order
	for _, hook := range rh.deleteHooks {
		if err := hook(rc); err != nil {
			return err
		}
	}

	if err := rc.Storage().Delete(rc.ObjectKey()); err != nil {
		return err
	}
	return rc.NoContent(http.StatusNoContent)
}

// customPartialObject is a superset of runtime.PartialObject that implements the
// Namespaced and Singleton interfaces in the way the Storage's identifier expects.
type customPartialObject struct {
	runtime.PartialObject
	namespaced bool
	singleton  bool
}

func (po *customPartialObject) IsNamespaced() bool { return po.namespaced }
func (po *customPartialObject) IsSingleton() bool  { return po.singleton }

// TODO: Upstream this
// namespaceFilter is an ObjectFilter that compares runtime.Object.GetNamespace()
// to the Namespace field.
type namespaceFilter struct {
	// Namespace matches the object by .metadata.namespace. If left as
	// an empty string, it is ignored when filtering.
	// +required
	Namespace string
}

// Filter implements ObjectFilter
func (f namespaceFilter) Filter(obj runtime.Object) (bool, error) {
	// Require f.Namespace to always be set.
	if len(f.Namespace) == 0 {
		return false, fmt.Errorf("the namespaceFilter.Namespace field must not be empty: %w", filter.ErrInvalidFilterParams)
	}
	// Otherwise, just use an equality check
	return f.Namespace == obj.GetNamespace(), nil
}

// ApplyToListOptions implements ListOption, and adds itself converted to
// a ListFilter to ListOptions.Filters.
func (f namespaceFilter) ApplyToListOptions(target *filter.ListOptions) error {
	target.Filters = append(target.Filters, filter.ObjectToListFilter(f))
	return nil
}
