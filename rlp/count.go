// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rlp

import (
	"fmt"
	//"io"
	"math/big"
	"reflect"
	//"sync"
)

// CountBytes returns the size of RLP encoding val.
// Please see package-level documentation for the encoding rules.
func CountBytes(val interface{}) (int, error) {
	counter := &countbuf{}
	if err := counter.doCount(val); err != nil {
		return 0, err
	}
	return counter.count, nil
}

type countbuf struct {
	count int
}

// reset clear it, in case anyone wants to reuse it
func (w *countbuf) reset() {
	w.count = 0
}

func (w *countbuf) doCount(val interface{}) error {
	rval := reflect.ValueOf(val)
	counter, err := cachedCounter(rval.Type())
	if err != nil {
		return err
	}
	return counter(rval, w)
}

func (w *countbuf) countString(b []byte) error {
	size := len(b)
	w.count += 1
	if size == 1 && b[0] <= 0x7F {
		return nil
	}
	w.count += size
	// and header
	if size >= 56 {
		w.count += intsize(uint64(size))
	}
	return nil
}

// makeCounter creates a counter function for the given type.
func makeCounter(typ reflect.Type, ts tags) (counter, error) {
	kind := typ.Kind()
	switch {
	case typ == rawValueType:
		return countRawValue, nil
	case typ.AssignableTo(reflect.PtrTo(bigInt)):
		return countBigIntPtr, nil
	case typ.AssignableTo(bigInt):
		return countBigIntNoPtr, nil
	case kind == reflect.Ptr:
		return makePtrCounter(typ, ts)
	case reflect.PtrTo(typ).Implements(encoderInterface):
		return makeEncoderCounter(typ), nil
	case isUint(kind):
		return countUint, nil
	case kind == reflect.Bool:
		return countBool, nil
	case kind == reflect.String:
		return countString, nil
	case kind == reflect.Slice && isByte(typ.Elem()):
		return countBytes, nil
	case kind == reflect.Array && isByte(typ.Elem()):
		return countByteArray, nil
	case kind == reflect.Slice || kind == reflect.Array:
		return makeSliceCounter(typ, ts)
	case kind == reflect.Struct:
		return makeStructCounter(typ)
	case kind == reflect.Interface:
		return countInterface, nil
	default:
		return nil, fmt.Errorf("rlp: type %v is not RLP-serializable", typ)
	}
}

func countRawValue(val reflect.Value, w *countbuf) error {
	w.count += len(val.Bytes())
	return nil
}

func countUint(val reflect.Value, w *countbuf) error {
	i := val.Uint()
	w.count += 1
	if i >= 128 {
		w.count += intsize(i)
	}
	return nil
}

func countBool(val reflect.Value, w *countbuf) error {
	w.count += 1
	return nil
}

func countBigIntPtr(val reflect.Value, w *countbuf) error {
	ptr := val.Interface().(*big.Int)
	if ptr == nil {
		w.count += 1
		return nil
	}
	return countBigInt(ptr, w)
}

func countBigIntNoPtr(val reflect.Value, w *countbuf) error {
	i := val.Interface().(big.Int)
	return countBigInt(&i, w)
}

func countBigInt(i *big.Int, w *countbuf) error {
	if cmp := i.Cmp(big0); cmp == -1 {
		return fmt.Errorf("rlp: cannot encode negative *big.Int")
	} else if cmp == 0 {
		w.count += 1
	} else {
		w.countString(i.Bytes())
	}
	return nil
}

func countBytes(val reflect.Value, w *countbuf) error {
	w.countString(val.Bytes())
	return nil
}

func countByteArray(val reflect.Value, w *countbuf) error {
	if !val.CanAddr() {
		// Slice requires the value to be addressable.
		// Make it addressable by copying.
		copy := reflect.New(val.Type()).Elem()
		copy.Set(val)
		val = copy
	}
	size := val.Len()
	slice := val.Slice(0, size).Bytes()
	w.countString(slice)
	return nil
}

func countString(val reflect.Value, w *countbuf) error {
	s := val.String()
	size := len(s)
	w.count += 1
	if size == 1 && s[0] <= 0x7f {
		// fits single byte, no string header
		return nil
	}
	// header
	w.count += size
	if size >= 56 {
		w.count += intsize(uint64(size))
	}
	return nil
}

func countInterface(val reflect.Value, w *countbuf) error {
	if val.IsNil() {
		// Write empty list. This is consistent with the previous RLP
		// encoder that we had and should therefore avoid any
		// problems.
		w.count += 1
		return nil
	}
	eval := val.Elem()
	counter, err := cachedCounter(eval.Type())
	if err != nil {
		return err
	}
	return counter(eval, w)
}

func makeSliceCounter(typ reflect.Type, ts tags) (counter, error) {
	etypeinfo := cachedTypeInfo1(typ.Elem(), tags{})
	if etypeinfo.writerErr != nil {
		return nil, etypeinfo.writerErr
	}
	counter := func(val reflect.Value, w *countbuf) error {
		offset := w.count
		for i := 0; i < val.Len(); i++ {
			if err := etypeinfo.counter(val.Index(i), w); err != nil {
				return err
			}
		}
		if !ts.tail {
			if finalSize := w.count - offset; finalSize >= 56 {
				w.count += intsize(uint64(finalSize))
			}
			w.count += 1
		}
		return nil
	}
	return counter, nil
}

func makeStructCounter(typ reflect.Type) (counter, error) {
	fields, err := structFields(typ)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if f.info.counterErr != nil {
			return nil, structFieldError{typ, f.index, f.info.counterErr}
		}
	}
	counter := func(val reflect.Value, w *countbuf) error {
		offset := w.count
		for _, f := range fields {
			if err := f.info.counter(val.Field(f.index), w); err != nil {
				return err
			}
		}
		// end of list
		if finalSize := w.count - offset; finalSize >= 56 {
			w.count += intsize(uint64(finalSize))
		}
		w.count += 1
		return nil
	}
	return counter, nil
}

func makePtrCounter(typ reflect.Type, ts tags) (counter, error) {
	etypeinfo := cachedTypeInfo1(typ.Elem(), tags{})
	if etypeinfo.writerErr != nil {
		return nil, etypeinfo.writerErr
	}
	// Determine how to encode nil pointers.
	var nilKind Kind
	if ts.nilOK {
		nilKind = ts.nilKind // use struct tag if provided
	} else {
		nilKind = defaultNilKind(typ.Elem())
	}

	counter := func(val reflect.Value, w *countbuf) error {
		if val.IsNil() {
			if nilKind == String {
				w.count++
			} else {
				w.count++
			}
			return nil
		}
		e := etypeinfo.counter(val.Elem(), w)
		return e
	}
	return counter, nil
}

// noopWriter just counts the data
type noopWriter struct {
	count int
}

func (nw *noopWriter) Write(p []byte) (n int, err error) {
	nw.count += len(p)
	return len(p), nil
}

func makeEncoderCounter(typ reflect.Type) counter {
	if typ.Implements(encoderInterface) {
		return func(val reflect.Value, w *countbuf) error {
			w2 := &noopWriter{}
			e := val.Interface().(Encoder).EncodeRLP(w2)
			w.count += w2.count
			return e
		}
	}
	c := func(val reflect.Value, w *countbuf) error {

		if !val.CanAddr() {
			// package json simply doesn't call MarshalJSON for this case, but encodes the
			// value as if it didn't implement the interface. We don't want to handle it that
			// way.
			return fmt.Errorf("rlp: unadressable value of type %v, EncodeRLP is pointer method", val.Type())
		}
		w2 := &noopWriter{}
		e := val.Addr().Interface().(Encoder).EncodeRLP(w2)
		w.count += w2.count
		return e
	}
	return c
}
