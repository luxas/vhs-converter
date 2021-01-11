package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/luxas/digitized/pkg/apis/digitized.luxaslabs.com/v1alpha1"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/luxas/digitized/pkg/rest"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

var (
	scheme = kruntime.NewScheme()

	ignoredGroups = map[schema.GroupVersion]struct{}{
		metav1.Unversioned: {},
	}
	ignoredKinds = map[schema.GroupVersionKind]struct{}{
		v1alpha1.SchemeGroupVersion.WithKind(v1alpha1.ConfigKind):              {},
		v1alpha1.SchemeGroupVersion.WithKind(v1alpha1.MergeCassetteActionKind): {},
		v1alpha1.SchemeGroupVersion.WithKind(v1alpha1.ListKind):                {},
	}
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("Usage: $0 [config file]")
	}
	return start(os.Args[1])
}

func start(cfgFile string) error {
	// Instantiate the serializer for the given scheme
	ser := serializer.NewSerializer(scheme, nil)

	// Decode the configuration for the app
	cfg := &v1alpha1.Config{}
	err := ser.Decoder().DecodeInto(
		serializer.NewYAMLFrameReader(
			serializer.FromFile(cfgFile)), cfg)
	if err != nil {
		return err
	}

	// Create a new simple storage
	s := storage.NewGenericStorage(
		storage.NewGenericRawStorage(storage.GenericRawStorageOptions{
			Directory:             cfg.RawStoragePath,
			ContentType:           serializer.ContentTypeYAML,
			DisableGroupDirectory: true,
			Namespacer:            namespacer{},
		}),
		ser,
		namespacer{},
	)

	// Create if it doesn't exist
	ctx := context.Background()
	if err := s.Create(ctx, &v1alpha1.Recorder{
		Spec: v1alpha1.RecorderSpec{
			Action: v1alpha1.RecorderActionNone,
		},
	}); err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		return err
	}

	// Create a new REST server for the storage
	r, err := rest.NewRESTServer(cfg.ListenAddress, s, ignoredGroups, ignoredKinds)
	if err != nil {
		return err
	}
	dr := &DigitizedRESTServer{r}
	dr.RegisterCustomRoutes()

	return dr.ListenBlocking()
}

var _ storage.Namespacer = &namespacer{}
var _ storage.NamespaceEnforcer = &namespacer{}

type namespacer struct{}

func (namespacer) IsNamespaced(gk schema.GroupKind) (bool, error) {
	gvs := scheme.PrioritizedVersionsForGroup(gk.Group)
	if len(gvs) == 0 {
		return false, errors.New("group has no version")
	}
	gvk := gk.WithVersion(gvs[0].Version)
	obj, err := scheme.New(gvk)
	if err != nil {
		return false, err
	}
	return meta.IsNamespaced(obj), nil
}

// RequireNamespaceExists specifies whether the namespace must exist in the system.
// Kubernetes requires this by default.
func (namespacer) RequireNamespaceExists() bool { return false }

func (namespacer) EnforceNamespace(obj storage.Object, namespaced bool, namespaces sets.String) error {
	if namespaced {
		// namespaced
		if obj.GetNamespace() == "" {
			return fmt.Errorf("a namespaced object must have namespace set")
		}
		return nil
	}
	// non-namespaced
	if obj.GetNamespace() != "" {
		// prune if exists
		obj.SetNamespace("")
	}
	return nil
}
