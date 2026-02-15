package middlewares

import "reflect"

func IsEmpty(i any) bool {
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
