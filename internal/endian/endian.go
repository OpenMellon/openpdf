// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package endian ;import (_e "encoding/binary";_ef "unsafe";);func IsBig ()bool {return _b };func init (){const _d =int (_ef .Sizeof (0));_eb :=1;_da :=(*[_d ]byte )(_ef .Pointer (&_eb ));if _da [0]==0{_b =true ;ByteOrder =_e .BigEndian ;}else {ByteOrder =_e .LittleEndian ;
};};var (ByteOrder _e .ByteOrder ;_b bool ;);func IsLittle ()bool {return !_b };