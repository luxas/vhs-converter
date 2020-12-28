package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/luxas/digitized/pkg/apis/digitized.luxaslabs.com/v1alpha1"
	"github.com/luxas/digitized/pkg/apis/meta"
	"github.com/luxas/digitized/pkg/rest"
	"github.com/weaveworks/libgitops/pkg/runtime"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
		storage.NewGenericRawStorage(cfg.RawStoragePath, v1alpha1.SchemeGroupVersion, serializer.ContentTypeYAML),
		ser,
		[]runtime.IdentifierFactory{meta.Metav1NameIdentifierFactory{}},
	)

	// Create if it doesn't exist
	if err := s.Create(&v1alpha1.Recorder{
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
