package topology

// Field represents an optional update value. Set distinguishes an omitted field
// from a field explicitly set to its zero value.
type Field[T any] struct {
	Set   bool
	Value T
}

func SetField[T any](value T) Field[T] {
	return Field[T]{Set: true, Value: value}
}
