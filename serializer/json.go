package serializer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Index     []int
	DecodePos int
	Name      string
	JSONName  []byte
	OmitEmpty bool
	Encoder   valueEncoder
}

type structMeta struct {
	Fields      []fieldMeta
	FieldByName map[string]fieldMeta
	DecodeType  reflect.Type
	UseNumber   bool
	SizeHint    int
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

	elem := target.Elem()
	if elem.Kind() == reflect.Struct {
		meta := getStructMeta(elem.Type(), u.Tag)
		decoded := reflect.New(meta.DecodeType).Elem()
		if meta.UseNumber {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(decoded.Addr().Interface()); err != nil {
				return err
			}
			if decoder.Decode(&struct{}{}) != io.EOF {
				return errors.New("invalid json")
			}
		} else if err := json.Unmarshal(data, decoded.Addr().Interface()); err != nil {
			return err
		}

		return copyDecodedStruct(decoded, elem, meta, u.Tag)
	}

	return u.assignRawValue(elem, data)
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
			fieldValue := value.FieldByIndex(field.Index)
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

func (u JSON) assignRawValue(target reflect.Value, raw json.RawMessage) error {
	if !target.CanSet() {
		return nil
	}

	if target.CanAddr() {
		if unmarshaler, ok := target.Addr().Interface().(json.Unmarshaler); ok {
			return unmarshaler.UnmarshalJSON(raw)
		}
	}

	if isRawNull(raw) {
		target.SetZero()
		return nil
	}

	switch target.Kind() {
	case reflect.Pointer:
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}

		return u.assignRawValue(target.Elem(), raw)
	case reflect.Struct:
		return u.assignRawStruct(target, raw)
	case reflect.Slice:
		if useStdJSONForType(target.Type().Elem()) {
			return json.Unmarshal(raw, target.Addr().Interface())
		}

		var sourceSlice []json.RawMessage
		if err := json.Unmarshal(raw, &sourceSlice); err != nil {
			return fmt.Errorf("expected array for %s: %w", target.Type(), err)
		}

		result := reflect.MakeSlice(target.Type(), len(sourceSlice), len(sourceSlice))
		for i := range sourceSlice {
			if err := u.assignRawValue(result.Index(i), sourceSlice[i]); err != nil {
				return err
			}
		}

		target.Set(result)
		return nil
	case reflect.Array:
		if useStdJSONForType(target.Type().Elem()) {
			return json.Unmarshal(raw, target.Addr().Interface())
		}

		var sourceSlice []json.RawMessage
		if err := json.Unmarshal(raw, &sourceSlice); err != nil {
			return fmt.Errorf("expected array for %s: %w", target.Type(), err)
		}

		limit := min(target.Len(), len(sourceSlice))
		for i := range limit {
			if err := u.assignRawValue(target.Index(i), sourceSlice[i]); err != nil {
				return err
			}
		}

		return nil
	case reflect.Map:
		if target.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("unsupported map key type: %s", target.Type().Key())
		}

		if useStdJSONForType(target.Type().Elem()) {
			return json.Unmarshal(raw, target.Addr().Interface())
		}

		sourceMap := make(map[string]json.RawMessage)
		if err := json.Unmarshal(raw, &sourceMap); err != nil {
			return fmt.Errorf("expected object for %s: %w", target.Type(), err)
		}

		result := reflect.MakeMapWithSize(target.Type(), len(sourceMap))
		for key, fieldRaw := range sourceMap {
			mapValue := reflect.New(target.Type().Elem()).Elem()
			if err := u.assignRawValue(mapValue, fieldRaw); err != nil {
				return err
			}

			result.SetMapIndex(reflect.ValueOf(key), mapValue)
		}

		target.Set(result)
		return nil
	case reflect.Interface:
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var source any
		if err := decoder.Decode(&source); err != nil {
			return err
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			return errors.New("invalid json")
		}
		target.Set(reflect.ValueOf(source))
		return nil
	default:
		return json.Unmarshal(raw, target.Addr().Interface())
	}
}

func (u JSON) assignRawStruct(target reflect.Value, raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))

	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("expected object for %s: %w", target.Type(), err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("expected object for %s", target.Type())
	}

	meta := getStructMeta(target.Type(), u.Tag)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return err
		}

		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("expected object key for %s", target.Type())
		}

		field, exists := meta.FieldByName[name]
		if !exists {
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return err
			}
			continue
		}

		if err := u.decodeValue(decoder, target.FieldByIndex(field.Index)); err != nil {
			return err
		}
	}

	token, err = decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return fmt.Errorf("expected object end for %s", target.Type())
	}

	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid json")
	}

	return nil
}

func (u JSON) decodeValue(decoder *json.Decoder, target reflect.Value) error {
	if !target.CanSet() {
		var skip json.RawMessage
		return decoder.Decode(&skip)
	}

	if requiresRawDecode(target) {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}

		return u.assignRawValue(target, raw)
	}

	return decoder.Decode(target.Addr().Interface())
}

func requiresRawDecode(target reflect.Value) bool {
	if target.CanAddr() {
		if _, ok := target.Addr().Interface().(json.Unmarshaler); ok {
			return true
		}
	}

	typ := target.Type()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	switch typ.Kind() {
	case reflect.Struct, reflect.Interface:
		return true
	case reflect.Slice, reflect.Array:
		elem := typ.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		return elem.Kind() == reflect.Struct || elem.Kind() == reflect.Interface
	case reflect.Map:
		elem := typ.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		return elem.Kind() == reflect.Struct || elem.Kind() == reflect.Interface
	default:
		return false
	}
}

func isRawNull(raw json.RawMessage) bool {
	return len(raw) == 4 && raw[0] == 'n' && raw[1] == 'u' && raw[2] == 'l' && raw[3] == 'l'
}

func useStdJSONForType(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	return typ.Kind() != reflect.Struct && typ.Kind() != reflect.Interface
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
		Fields:      make([]fieldMeta, 0, typ.NumField()),
		FieldByName: make(map[string]fieldMeta, typ.NumField()),
	}

	seen := make(map[string]struct{}, typ.NumField())

	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}

		rawTag := field.Tag.Get(tag)
		name, skip, omitempty := fieldTagMeta(field, tag)
		if skip {
			continue
		}

		// Anonymous embedded struct without an explicit name in the selected tag:
		// treat as "inlined" (promoted) fields, like encoding/json does for json tags.
		// Outer struct fields win on name conflicts.
		if field.Anonymous && rawTag == "" {
			embeddedType := field.Type
			for embeddedType.Kind() == reflect.Pointer {
				embeddedType = embeddedType.Elem()
			}
			if embeddedType.Kind() == reflect.Struct {
				embeddedMeta := getStructMeta(embeddedType, tag)
				for _, ef := range embeddedMeta.Fields {
					if _, ok := seen[ef.Name]; ok {
						continue
					}
					seen[ef.Name] = struct{}{}

					idx := make([]int, 0, 1+len(ef.Index))
					idx = append(idx, i)
					idx = append(idx, ef.Index...)
					built.Fields = append(built.Fields, fieldMeta{
						Index:     idx,
						Name:      ef.Name,
						JSONName:  ef.JSONName,
						OmitEmpty: ef.OmitEmpty,
						Encoder:   ef.Encoder,
					})
					built.FieldByName[ef.Name] = built.Fields[len(built.Fields)-1]
					built.SizeHint += len(ef.JSONName) + 2
				}
				continue
			}
		}

		jsonName := []byte(strconv.Quote(name))
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		built.Fields = append(built.Fields, fieldMeta{
			Index:     []int{i},
			Name:      name,
			JSONName:  jsonName,
			OmitEmpty: omitempty,
			Encoder:   encoderForType(field.Type),
		})
		built.FieldByName[name] = built.Fields[len(built.Fields)-1]
		built.SizeHint += len(jsonName) + 2
	}

	decodeFields := make([]reflect.StructField, len(built.Fields))
	for i := range built.Fields {
		built.Fields[i].DecodePos = i
		built.FieldByName[built.Fields[i].Name] = built.Fields[i]

		fieldType := typ.FieldByIndex(built.Fields[i].Index).Type
		if typeNeedsUseNumber(fieldType, tag) {
			built.UseNumber = true
		}
		decodeFields[i] = reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: decodeTypeFor(fieldType, tag),
			Tag:  reflect.StructTag(`json:"` + built.Fields[i].Name + `"`),
		}
	}
	if len(decodeFields) == 0 {
		built.DecodeType = reflect.StructOf([]reflect.StructField{})
	} else {
		built.DecodeType = reflect.StructOf(decodeFields)
	}

	if len(built.Fields) > 0 {
		built.SizeHint += len(built.Fields) - 1
	}
	built.SizeHint += 2

	actual, _ := structMetaCache.LoadOrStore(key, built)
	return actual.(*structMeta)
}

func decodeTypeFor(typ reflect.Type, tag string) reflect.Type {
	switch typ.Kind() {
	case reflect.Pointer:
		return reflect.PointerTo(decodeTypeFor(typ.Elem(), tag))
	case reflect.Struct:
		if reflect.PointerTo(typ).Implements(unmarshalerType) || typ.Implements(unmarshalerType) {
			return typ
		}
		return getStructMeta(typ, tag).DecodeType
	case reflect.Slice:
		return reflect.SliceOf(decodeTypeFor(typ.Elem(), tag))
	case reflect.Array:
		return reflect.ArrayOf(typ.Len(), decodeTypeFor(typ.Elem(), tag))
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return typ
		}
		return reflect.MapOf(typ.Key(), decodeTypeFor(typ.Elem(), tag))
	default:
		return typ
	}
}

var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

func typeNeedsUseNumber(typ reflect.Type, tag string) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	switch typ.Kind() {
	case reflect.Interface:
		return true
	case reflect.Struct:
		if reflect.PointerTo(typ).Implements(unmarshalerType) || typ.Implements(unmarshalerType) {
			return false
		}
		return getStructMeta(typ, tag).UseNumber
	case reflect.Slice, reflect.Array:
		return typeNeedsUseNumber(typ.Elem(), tag)
	case reflect.Map:
		return typeNeedsUseNumber(typ.Elem(), tag)
	default:
		return false
	}
}

func copyDecodedStruct(source reflect.Value, target reflect.Value, meta *structMeta, tag string) error {
	for _, field := range meta.Fields {
		if err := copyDecodedValue(source.Field(field.DecodePos), target.FieldByIndex(field.Index), tag); err != nil {
			return err
		}
	}

	return nil
}

func copyDecodedValue(source reflect.Value, target reflect.Value, tag string) error {
	if !target.CanSet() {
		return nil
	}
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return nil
	}

	switch target.Kind() {
	case reflect.Pointer:
		if source.IsNil() {
			target.SetZero()
			return nil
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		return copyDecodedValue(source.Elem(), target.Elem(), tag)
	case reflect.Struct:
		meta := getStructMeta(target.Type(), tag)
		return copyDecodedStruct(source, target, meta, tag)
	case reflect.Slice:
		if source.IsNil() {
			target.SetZero()
			return nil
		}
		result := reflect.MakeSlice(target.Type(), source.Len(), source.Len())
		for i := range source.Len() {
			if err := copyDecodedValue(source.Index(i), result.Index(i), tag); err != nil {
				return err
			}
		}
		target.Set(result)
		return nil
	case reflect.Array:
		limit := min(source.Len(), target.Len())
		for i := range limit {
			if err := copyDecodedValue(source.Index(i), target.Index(i), tag); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if source.IsNil() {
			target.SetZero()
			return nil
		}
		result := reflect.MakeMapWithSize(target.Type(), source.Len())
		iter := source.MapRange()
		for iter.Next() {
			mapValue := reflect.New(target.Type().Elem()).Elem()
			if err := copyDecodedValue(iter.Value(), mapValue, tag); err != nil {
				return err
			}
			result.SetMapIndex(iter.Key(), mapValue)
		}
		target.Set(result)
		return nil
	default:
		if source.Type().ConvertibleTo(target.Type()) {
			target.Set(source.Convert(target.Type()))
			return nil
		}
		return fmt.Errorf("cannot assign decoded %s to %s", source.Type(), target.Type())
	}
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
	return appendArray(buf, value, func(buf []byte, item reflect.Value) []byte {
		return strconv.AppendQuote(buf, item.String())
	})
}

func appendBoolArray(buf []byte, value reflect.Value) []byte {
	return appendArray(buf, value, func(buf []byte, item reflect.Value) []byte {
		return strconv.AppendBool(buf, item.Bool())
	})
}

func appendIntArray(buf []byte, value reflect.Value) []byte {
	return appendArray(buf, value, func(buf []byte, item reflect.Value) []byte {
		return strconv.AppendInt(buf, item.Int(), 10)
	})
}

func appendUintArray(buf []byte, value reflect.Value) []byte {
	return appendArray(buf, value, func(buf []byte, item reflect.Value) []byte {
		return strconv.AppendUint(buf, item.Uint(), 10)
	})
}

func appendFloatArray(buf []byte, value reflect.Value, bitSize int) []byte {
	return appendArray(buf, value, func(buf []byte, item reflect.Value) []byte {
		return strconv.AppendFloat(buf, item.Float(), 'g', -1, bitSize)
	})
}

func appendArray(buf []byte, value reflect.Value, appendItem func([]byte, reflect.Value) []byte) []byte {
	buf = append(buf, '[')
	for i := range value.Len() {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendItem(buf, value.Index(i))
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
	number, err := toNumber(value)
	if err != nil {
		return 0, err
	}

	return number.Int64()
}

func toUint64(value any) (uint64, error) {
	asInt, err := toInt64(value)
	if err != nil {
		return 0, err
	}
	if asInt < 0 {
		return 0, fmt.Errorf("negative number for unsigned value: %d", asInt)
	}

	return uint64(asInt), nil
}

func toFloat64(value any) (float64, error) {
	number, err := toNumber(value)
	if err != nil {
		return 0, err
	}

	return number.Float64()
}

func toNumber(value any) (json.Number, error) {
	switch v := value.(type) {
	case json.Number:
		return v, nil
	case float64:
		return json.Number(strconv.FormatFloat(v, 'g', -1, 64)), nil
	default:
		return "", fmt.Errorf("expected number, got %T", value)
	}
}
