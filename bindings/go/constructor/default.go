package constructor

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	// Scheme is the default runtime scheme for the registry
	Scheme = runtime.NewScheme()
	// DefaultInputMethodRegistry is the default registry instance using the default scheme
	DefaultInputMethodRegistry = New(Scheme)
)

// MustAddToScheme registers the file and UTF8 types with the given scheme.
// It registers both versioned and unversioned types.
func MustAddToScheme(scheme *runtime.Scheme) {
	// scheme.MustRegisterWithAlias(&filev1.File{},
	// 	runtime.NewVersionedType("file", filev1.Version),
	// 	runtime.NewUnversionedType("file"),
	// )
	//
	// scheme.MustRegisterWithAlias(&v2alpha1.UTF8{},
	// 	runtime.NewVersionedType("utf8", v2alpha1.Version),
	// 	runtime.NewUnversionedType("utf8"),
	// )
	//
	// scheme.MustRegisterWithAlias(&helmv1.Helm{},
	// 	runtime.NewVersionedType("helmChart", helmv1.Version),
	// 	runtime.NewUnversionedType("helmChart"),
	// 	runtime.NewVersionedType("helm", helmv1.Version),
	// 	runtime.NewUnversionedType("helm"),
	// )
}

func init() {
	MustAddToScheme(Scheme)
	// DefaultInputMethodRegistry.MustRegisterMethod(&filev1.File{}, &file.Method{Scheme: Scheme})
	// DefaultInputMethodRegistry.MustRegisterMethod(&v2alpha1.UTF8{}, &utf8.Method{Scheme: Scheme})
	// DefaultInputMethodRegistry.MustRegisterMethod(&helmv1.Helm{}, &helm.Method{Scheme: Scheme})
}
