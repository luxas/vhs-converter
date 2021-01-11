package meta

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

// SingletonSpecialName specifies the .metadata.name for the singleton to have
// when passed around in the system. However, ObjectMeta is not marshalled in
// singleton objects, so this should not "leak" outside the system, but influences
// e.g. the file path in RawStorage.
const SingletonSpecialName = "__singleton"
