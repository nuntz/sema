package main

import (
	"encoding/json"
	"reflect"
)

var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// normalizeNilJSONSlices ensures collection fields cross the API boundary as
// JSON arrays. Pointers, maps, byte slices, and other nullable values retain
// their normal JSON null semantics.
func normalizeNilJSONSlices(body any) any {
	if body == nil {
		return nil
	}
	return normalizeJSONValue(reflect.ValueOf(body)).Interface()
}

func normalizeJSONValue(value reflect.Value) reflect.Value {
	if !value.IsValid() || implementsJSONMarshaler(value.Type()) {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		normalized := normalizeJSONValue(value.Elem())
		result := reflect.New(value.Type()).Elem()
		result.Set(normalized)
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(normalizeJSONValue(value.Elem()))
		return result
	case reflect.Slice:
		if value.IsNil() {
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return value
			}
			return reflect.MakeSlice(value.Type(), 0, 0)
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(normalizeJSONValue(value.Index(index)))
		}
		return result
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(normalizeJSONValue(value.Index(index)))
		}
		return result
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), normalizeJSONValue(iterator.Value()))
		}
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		result.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath == "" {
				result.Field(index).Set(normalizeJSONValue(value.Field(index)))
			}
		}
		return result
	default:
		return value
	}
}

func implementsJSONMarshaler(valueType reflect.Type) bool {
	if valueType.Implements(jsonMarshalerType) {
		return true
	}
	return valueType.Kind() != reflect.Pointer && reflect.PointerTo(valueType).Implements(jsonMarshalerType)
}
