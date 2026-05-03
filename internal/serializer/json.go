package serializer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

type JSON struct {
	Tag string
}

type structCacheKey struct {
	Type reflect.Type
	Tag  string
}

type fieldMeta struct {
	Index     int
	Name      string
	JSONName  []byte
	OmitEmpty bool
	Encoder   valueEncoder
}

type structMeta struct {
	Fields   []fieldMeta
	SizeHint int
}

type valueEncoder func(buf []byte, value reflect.Value, codec JSON) ([]byte, error)

var (
	structMetaCache  sync.Map
	encodeBufferPool = sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 512)
			return &buf
		},
	}
)

func Marshal(v any) ([]byte, error) {
	return New("json").Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return New("json").Unmarshal(data, v)
}

func MarshalWithTag(v any, structTag string) ([]byte, error) {
	return New(structTag).Marshal(v)
}

func UnmarshalWithTag(data []byte, v any, structTag string) error {
	return New(structTag).Unmarshal(data, v)
}

func New(structTag string) *JSON {
	if structTag == "" {
		structTag = "json"
	}

	return &JSON{Tag: structTag}
}

func (u JSON) Marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, errors.New("marshal nil value")
	}

	if u.Tag == "json" {
		return json.Marshal(v)
	}

	value := reflect.ValueOf(v)
	bufPtr := encodeBufferPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	if capacity := u.bufferCapacity(value); cap(buf) < capacity {
		buf = make([]byte, 0, capacity)
	}

	buf, err := u.appendValue(buf, value)
	if err != nil {
		returnEncodeBuffer(bufPtr, buf)
		return nil, err
	}

	output := make([]byte, len(buf))
	copy(output, buf)
	returnEncodeBuffer(bufPtr, buf)

	return output, nil
}

func (u JSON) Unmarshal(data []byte, v any) error {
	if v == nil {
		return errors.New("unmarshal target is nil")
	}

	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("unmarshal expects non-nil pointer")
	}

	if u.Tag == "json" {
		return json.Unmarshal(data, v)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return err
	}

	return u.assignValue(target.Elem(), raw)
}

func (u JSON) appendValue(buf []byte, value reflect.Value) ([]byte, error) {
	if !value.IsValid() {
		return append(buf, "null"...), nil
	}

	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return append(buf, "null"...), nil
		}

		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		if value.CanInterface() {
			if marshaler, ok := value.Interface().(json.Marshaler); ok {
				encoded, err := marshaler.MarshalJSON()
				if err != nil {
					return nil, err
				}

				return append(buf, encoded...), nil
			}
		}

		meta := getStructMeta(value.Type(), u.Tag)
		buf = append(buf, '{')
		wroteField := false

		for _, field := range meta.Fields {
			fieldValue := value.Field(field.Index)
			if field.OmitEmpty && isZero(fieldValue) {
				continue
			}

			if wroteField {
				buf = append(buf, ',')
			}
			wroteField = true

			buf = append(buf, field.JSONName...)
			buf = append(buf, ':')

			var err error
			buf, err = field.Encoder(buf, fieldValue, u)
			if err != nil {
				return nil, err
			}
		}

		buf = append(buf, '}')
		return buf, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return append(buf, "null"...), nil
		}

		switch value.Type().Elem().Kind() {
		case reflect.String:
			return appendStringArray(buf, value), nil
		case reflect.Bool:
			return appendBoolArray(buf, value), nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return appendIntArray(buf, value), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return appendUintArray(buf, value), nil
		case reflect.Float32:
			return appendFloatArray(buf, value, 32), nil
		case reflect.Float64:
			return appendFloatArray(buf, value, 64), nil
		}

		buf = append(buf, '[')
		for i := range value.Len() {
			if i > 0 {
				buf = append(buf, ',')
			}

			var err error
			buf, err = u.appendValue(buf, value.Index(i))
			if err != nil {
				return nil, err
			}
		}

		buf = append(buf, ']')
		return buf, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return appendJSONMarshal(buf, value.Interface())
		}

		buf = append(buf, '{')
		iter := value.MapRange()
		wroteField := false
		for iter.Next() {
			if wroteField {
				buf = append(buf, ',')
			}
			wroteField = true

			buf = strconv.AppendQuote(buf, iter.Key().String())
			buf = append(buf, ':')

			var err error
			buf, err = u.appendValue(buf, iter.Value())
			if err != nil {
				return nil, err
			}
		}

		buf = append(buf, '}')
		return buf, nil
	case reflect.String:
		return strconv.AppendQuote(buf, value.String()), nil
	case reflect.Bool:
		return strconv.AppendBool(buf, value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(buf, value.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.AppendUint(buf, value.Uint(), 10), nil
	case reflect.Float32:
		return strconv.AppendFloat(buf, value.Float(), 'g', -1, 32), nil
	case reflect.Float64:
		return strconv.AppendFloat(buf, value.Float(), 'g', -1, 64), nil
	default:
		return appendJSONMarshal(buf, value.Interface())
	}
}

func (u JSON) assignValue(target reflect.Value, source any) error {
	if !target.CanSet() {
		return nil
	}

	if target.CanAddr() {
		if unmarshaler, ok := target.Addr().Interface().(json.Unmarshaler); ok {
			raw, err := json.Marshal(source)
			if err != nil {
				return err
			}

			return unmarshaler.UnmarshalJSON(raw)
		}
	}

	if source == nil {
		target.SetZero()
		return nil
	}

	switch target.Kind() {
	case reflect.Pointer:
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}

		return u.assignValue(target.Elem(), source)
	case reflect.Struct:
		sourceMap, ok := source.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for %s, got %T", target.Type(), source)
		}

		meta := getStructMeta(target.Type(), u.Tag)
		for _, field := range meta.Fields {
			raw, exists := sourceMap[field.Name]
			if !exists {
				continue
			}

			if err := u.assignValue(target.Field(field.Index), raw); err != nil {
				return err
			}
		}

		return nil
	case reflect.Slice:
		sourceSlice, ok := source.([]any)
		if !ok {
			return fmt.Errorf("expected array for %s, got %T", target.Type(), source)
		}

		result := reflect.MakeSlice(target.Type(), len(sourceSlice), len(sourceSlice))
		for i := range sourceSlice {
			if err := u.assignValue(result.Index(i), sourceSlice[i]); err != nil {
				return err
			}
		}

		target.Set(result)
		return nil
	case reflect.Array:
		sourceSlice, ok := source.([]any)
		if !ok {
			return fmt.Errorf("expected array for %s, got %T", target.Type(), source)
		}

		limit := min(target.Len(), len(sourceSlice))
		for i := range limit {
			if err := u.assignValue(target.Index(i), sourceSlice[i]); err != nil {
				return err
			}
		}

		return nil
	case reflect.Map:
		if target.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("unsupported map key type: %s", target.Type().Key())
		}

		sourceMap, ok := source.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for %s, got %T", target.Type(), source)
		}

		result := reflect.MakeMapWithSize(target.Type(), len(sourceMap))
		for key, raw := range sourceMap {
			mapValue := reflect.New(target.Type().Elem()).Elem()
			if err := u.assignValue(mapValue, raw); err != nil {
				return err
			}

			result.SetMapIndex(reflect.ValueOf(key), mapValue)
		}

		target.Set(result)
		return nil
	case reflect.Interface:
		target.Set(reflect.ValueOf(source))
		return nil
	case reflect.String:
		value, ok := source.(string)
		if !ok {
			return fmt.Errorf("expected string for %s, got %T", target.Type(), source)
		}

		target.SetString(value)
		return nil
	case reflect.Bool:
		value, ok := source.(bool)
		if !ok {
			return fmt.Errorf("expected bool for %s, got %T", target.Type(), source)
		}

		target.SetBool(value)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := toInt64(source)
		if err != nil {
			return err
		}

		target.SetInt(value)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value, err := toUint64(source)
		if err != nil {
			return err
		}

		target.SetUint(value)
		return nil
	case reflect.Float32, reflect.Float64:
		value, err := toFloat64(source)
		if err != nil {
			return err
		}

		target.SetFloat(value)
		return nil
	default:
		raw, err := json.Marshal(source)
		if err != nil {
			return err
		}

		return json.Unmarshal(raw, target.Addr().Interface())
	}
}

func fieldTagMeta(field reflect.StructField, tag string) (name string, skip bool, omitempty bool) {
	rawTag := field.Tag.Get(tag)
	parts := strings.Split(rawTag, ",")

	name = parts[0]
	if name == "-" {
		return "", true, false
	}
	if name == "" {
		name = field.Name
	}

	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitempty = true
		}
	}

	return name, false, omitempty
}

func getStructMeta(typ reflect.Type, tag string) *structMeta {
	key := structCacheKey{Type: typ, Tag: tag}
	if cached, ok := structMetaCache.Load(key); ok {
		return cached.(*structMeta)
	}

	built := &structMeta{
		Fields: make([]fieldMeta, 0, typ.NumField()),
	}

	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}

		name, skip, omitempty := fieldTagMeta(field, tag)
		if skip {
			continue
		}

		jsonName := []byte(strconv.Quote(name))
		built.Fields = append(built.Fields, fieldMeta{
			Index:     i,
			Name:      name,
			JSONName:  jsonName,
			OmitEmpty: omitempty,
			Encoder:   encoderForType(field.Type),
		})
		built.SizeHint += len(jsonName) + 2
	}

	if len(built.Fields) > 0 {
		built.SizeHint += len(built.Fields) - 1
	}
	built.SizeHint += 2

	actual, _ := structMetaCache.LoadOrStore(key, built)
	return actual.(*structMeta)
}

func (u JSON) bufferCapacity(value reflect.Value) int {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return 4
		}

		value = value.Elem()
	}

	if !value.IsValid() {
		return 4
	}
	if value.Kind() != reflect.Struct {
		return 64
	}

	meta := getStructMeta(value.Type(), u.Tag)
	return meta.SizeHint + 64
}

func returnEncodeBuffer(bufPtr *[]byte, buf []byte) {
	if cap(buf) > 64*1024 {
		return
	}

	*bufPtr = buf[:0]
	encodeBufferPool.Put(bufPtr)
}

func encoderForType(typ reflect.Type) valueEncoder {
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Interface {
		return encodeDefaultValue
	}

	switch typ.Kind() {
	case reflect.String:
		return encodeStringValue
	case reflect.Bool:
		return encodeBoolValue
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return encodeIntValue
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return encodeUintValue
	case reflect.Float32:
		return encodeFloat32Value
	case reflect.Float64:
		return encodeFloat64Value
	case reflect.Slice, reflect.Array:
		return encodeArrayValue
	}

	return encodeDefaultValue
}

func encodeDefaultValue(buf []byte, value reflect.Value, codec JSON) ([]byte, error) {
	return codec.appendValue(buf, value)
}

func encodeStringValue(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendQuote(buf, dereferenceValue(value).String()), nil
}

func encodeBoolValue(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendBool(buf, dereferenceValue(value).Bool()), nil
}

func encodeIntValue(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendInt(buf, dereferenceValue(value).Int(), 10), nil
}

func encodeUintValue(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendUint(buf, dereferenceValue(value).Uint(), 10), nil
}

func encodeFloat32Value(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendFloat(buf, dereferenceValue(value).Float(), 'g', -1, 32), nil
}

func encodeFloat64Value(buf []byte, value reflect.Value, _ JSON) ([]byte, error) {
	return strconv.AppendFloat(buf, dereferenceValue(value).Float(), 'g', -1, 64), nil
}

func encodeArrayValue(buf []byte, value reflect.Value, codec JSON) ([]byte, error) {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return append(buf, "null"...), nil
	}

	value = dereferenceValue(value)
	switch value.Type().Elem().Kind() {
	case reflect.String:
		return appendStringArray(buf, value), nil
	case reflect.Bool:
		return appendBoolArray(buf, value), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendIntArray(buf, value), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return appendUintArray(buf, value), nil
	case reflect.Float32:
		return appendFloatArray(buf, value, 32), nil
	case reflect.Float64:
		return appendFloatArray(buf, value, 64), nil
	default:
		return codec.appendValue(buf, value)
	}
}

func dereferenceValue(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		value = value.Elem()
	}

	return value
}

func appendJSONMarshal(buf []byte, value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return append(buf, encoded...), nil
}

func appendStringArray(buf []byte, value reflect.Value) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = strconv.AppendQuote(buf, value.Index(i).String())
	}

	return append(buf, ']')
}

func appendBoolArray(buf []byte, value reflect.Value) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = strconv.AppendBool(buf, value.Index(i).Bool())
	}

	return append(buf, ']')
}

func appendIntArray(buf []byte, value reflect.Value) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = strconv.AppendInt(buf, value.Index(i).Int(), 10)
	}

	return append(buf, ']')
}

func appendUintArray(buf []byte, value reflect.Value) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = strconv.AppendUint(buf, value.Index(i).Uint(), 10)
	}

	return append(buf, ']')
}

func appendFloatArray(buf []byte, value reflect.Value, bitSize int) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = strconv.AppendFloat(buf, value.Index(i).Float(), 'g', -1, bitSize)
	}

	return append(buf, ']')
}

func isZero(v reflect.Value) bool {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return true
		}

		v = v.Elem()
	}

	return v.IsZero()
}

func toInt64(value any) (int64, error) {
	switch v := value.(type) {
	case json.Number:
		return v.Int64()
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func toUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case json.Number:
		asInt, err := v.Int64()
		if err != nil {
			return 0, err
		}
		if asInt < 0 {
			return 0, fmt.Errorf("negative number for unsigned value: %d", asInt)
		}

		return uint64(asInt), nil
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative number for unsigned value: %f", v)
		}

		return uint64(v), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func toFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case json.Number:
		return v.Float64()
	case float64:
		return v, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}
