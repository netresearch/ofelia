package middlewares

import "reflect"

func IsEmpty(i interface{}) bool {
	t := reflect.TypeOf(i).Elem()
	e := reflect.New(t).Interface()

	return reflect.DeepEqual(i, e)
}

// boolVal safely dereferences a *bool, returning false when nil.
func boolVal(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// BoolPtr returns a pointer to the given bool value.
// Used in config merging and tests to create explicit *bool values.
func BoolPtr(v bool) *bool {
	return &v
}
