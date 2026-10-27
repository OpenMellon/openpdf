// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package arithmetic

import (
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	jbig2internal "github.com/OpenMellon/openpdf/internal/jbig2/internal"
	"io"
	"strings"
)

func (s *DecoderStats) String() string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "Stats:  %d\n", len(s.contexts))
	for i, v := range s.contexts {
		if v != 0 {
			fmt.Fprintf(sb, "Not zero at: %d - %d\n", i, v)
		}
	}
	return sb.String()
}
func New(r *bitwise.Reader) (*Decoder, error) {
	d := &Decoder{reader: r, ContextSize: []uint32{16, 13, 10, 10}, ReferedToContextSize: []uint32{13, 10}}
	if err := d.init(); err != nil {
		return nil, err
	}
	return d, nil
}
func (d *Decoder) DecodeInt(stats *DecoderStats) (int32, error) {
	var (
		value, offset    int32
		bit, sign, nbits int
		err              error
	)
	if stats == nil {
		stats = NewStats(512, 1)
	}
	d.prev = 1
	sign, err = d.decodeIntBit(stats)
	if err != nil {
		return 0, err
	}
	bit, err = d.decodeIntBit(stats)
	if err != nil {
		return 0, err
	}
	if bit == 1 {
		bit, err = d.decodeIntBit(stats)
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			bit, err = d.decodeIntBit(stats)
			if err != nil {
				return 0, err
			}
			if bit == 1 {
				bit, err = d.decodeIntBit(stats)
				if err != nil {
					return 0, err
				}
				if bit == 1 {
					bit, err = d.decodeIntBit(stats)
					if err != nil {
						return 0, err
					}
					if bit == 1 {
						nbits = 32
						offset = 4436
					} else {
						nbits = 12
						offset = 340
					}
				} else {
					nbits = 8
					offset = 84
				}
			} else {
				nbits = 6
				offset = 20
			}
		} else {
			nbits = 4
			offset = 4
		}
	} else {
		nbits = 2
		offset = 0
	}
	for i := 0; i < nbits; i++ {
		bit, err = d.decodeIntBit(stats)
		if err != nil {
			return 0, err
		}
		value = (value << 1) | int32(bit)
	}
	value += offset
	if sign == 0 {
		return value, nil
	} else if sign == 1 && value > 0 {
		return -value, nil
	}
	return 0, jbig2internal.ErrOOB
}
func (d *Decoder) mpsExchange(stats *DecoderStats, index int32) int {
	mps := stats.mps[stats.index]
	if d.a < qeTable[index][0] {
		if qeTable[index][3] == 1 {
			stats.toggleMps()
		}
		stats.setEntry(int(qeTable[index][2]))
		return int(1 - mps)
	}
	stats.setEntry(int(qeTable[index][1]))
	return int(mps)
}

var (
	qeTable = [][4]uint32{{0x5601, 1, 1, 1}, {0x3401, 2, 6, 0}, {0x1801, 3, 9, 0}, {0x0AC1, 4, 12, 0}, {0x0521, 5, 29, 0}, {0x0221, 38, 33, 0}, {0x5601, 7, 6, 1}, {0x5401, 8, 14, 0}, {0x4801, 9, 14, 0}, {0x3801, 10, 14, 0}, {0x3001, 11, 17, 0}, {0x2401, 12, 18, 0}, {0x1C01, 13, 20, 0}, {0x1601, 29, 21, 0}, {0x5601, 15, 14, 1}, {0x5401, 16, 14, 0}, {0x5101, 17, 15, 0}, {0x4801, 18, 16, 0}, {0x3801, 19, 17, 0}, {0x3401, 20, 18, 0}, {0x3001, 21, 19, 0}, {0x2801, 22, 19, 0}, {0x2401, 23, 20, 0}, {0x2201, 24, 21, 0}, {0x1C01, 25, 22, 0}, {0x1801, 26, 23, 0}, {0x1601, 27, 24, 0}, {0x1401, 28, 25, 0}, {0x1201, 29, 26, 0}, {0x1101, 30, 27, 0}, {0x0AC1, 31, 28, 0}, {0x09C1, 32, 29, 0}, {0x08A1, 33, 30, 0}, {0x0521, 34, 31, 0}, {0x0441, 35, 32, 0}, {0x02A1, 36, 33, 0}, {0x0221, 37, 34, 0}, {0x0141, 38, 35, 0}, {0x0111, 39, 36, 0}, {0x0085, 40, 37, 0}, {0x0049, 41, 38, 0}, {0x0025, 42, 39, 0}, {0x0015, 43, 40, 0}, {0x0009, 44, 41, 0}, {0x0005, 45, 42, 0}, {0x0001, 45, 43, 0}, {0x5601, 46, 46, 0}}
)

func (s *DecoderStats) toggleMps() { s.mps[s.index] ^= 1 }
func (d *Decoder) init() error {
	d.startPos = d.reader.AbsolutePosition()
	b, err := d.reader.ReadByte()
	if err != nil {
		common.Log.Debug("Buffer0 readByte failed. %v", err)
		return err
	}
	d.currentByte = b
	d.c = uint64(b) << 16
	if err = d.readByte(); err != nil {
		return err
	}
	d.c <<= 7
	d.ct -= 7
	d.a = 0x8000
	d.bitsDecoded++
	return nil
}
func (d *Decoder) DecodeIAID(codeLen uint64, stats *DecoderStats) (int64, error) {
	d.prev = 1
	var i uint64
	for i = 0; i < codeLen; i++ {
		stats.SetIndex(int32(d.prev))
		bit, err := d.DecodeBit(stats)
		if err != nil {
			return 0, err
		}
		d.prev = (d.prev << 1) | int64(bit)
	}
	result := d.prev - (1 << codeLen)
	return result, nil
}
func (s *DecoderStats) Copy() *DecoderStats {
	c := &DecoderStats{contextSize: s.contextSize, contexts: make([]byte, s.contextSize)}
	copy(c.contexts, s.contexts)
	return c
}
func (s *DecoderStats) cx() byte { return s.contexts[s.index] }
func (d *Decoder) decodeIntBit(stats *DecoderStats) (int, error) {
	stats.SetIndex(int32(d.prev))
	bit, err := d.DecodeBit(stats)
	if err != nil {
		common.Log.Debug("ArithmeticDecoder 'decodeIntBit'-> DecodeBit failed. %v", err)
		return bit, err
	}
	if d.prev < 256 {
		d.prev = ((d.prev << uint64(1)) | int64(bit)) & 0x1ff
	} else {
		d.prev = (((d.prev<<uint64(1) | int64(bit)) & 511) | 256) & 0x1ff
	}
	return bit, nil
}
func (s *DecoderStats) Overwrite(dNew *DecoderStats) {
	for i := 0; i < len(s.contexts); i++ {
		s.contexts[i] = dNew.contexts[i]
		s.mps[i] = dNew.mps[i]
	}
}
func (d *Decoder) DecodeBit(stats *DecoderStats) (int, error) {
	var (
		bit   int
		qe    = qeTable[stats.cx()][0]
		index = int32(stats.cx())
	)
	defer func() { d.bitsDecoded++ }()
	d.a -= qe
	if (d.c >> 16) < uint64(qe) {
		bit = d.lpsExchange(stats, index, qe)
		if err := d.renormalize(); err != nil {
			return 0, err
		}
	} else {
		d.c -= uint64(qe) << 16
		if (d.a & 0x8000) == 0 {
			bit = d.mpsExchange(stats, index)
			if err := d.renormalize(); err != nil {
				return 0, err
			}
		} else {
			bit = int(stats.getMps())
		}
	}
	return bit, nil
}

type Decoder struct {
	ContextSize          []uint32
	ReferedToContextSize []uint32
	reader               *bitwise.Reader
	currentByte          uint8
	c                    uint64
	a                    uint32
	prev                 int64
	ct                   int32
	bitsDecoded          int32
	startPos             int64
}

func (s *DecoderStats) getMps() byte         { return s.mps[s.index] }
func (s *DecoderStats) SetIndex(index int32) { s.index = index }
func (d *Decoder) lpsExchange(stats *DecoderStats, index int32, qe uint32) int {
	mps := stats.getMps()
	if d.a < qe {
		stats.setEntry(int(qeTable[index][1]))
		d.a = qe
		return int(mps)
	}
	if qeTable[index][3] == 1 {
		stats.toggleMps()
	}
	stats.setEntry(int(qeTable[index][2]))
	d.a = qe
	return int(1 - mps)
}
func (d *Decoder) readByte() error {
	if d.reader.AbsolutePosition() > d.startPos {
		if _, err := d.reader.Seek(-1, io.SeekCurrent); err != nil {
			return err
		}
	}
	b, err := d.reader.ReadByte()
	if err != nil {
		return err
	}
	d.currentByte = b
	if d.currentByte == 0xFF {
		next, err := d.reader.ReadByte()
		if err != nil {
			return err
		}
		if next > 0x8F {
			d.c += 0xFF00
			d.ct = 8
			if _, err := d.reader.Seek(-2, io.SeekCurrent); err != nil {
				return err
			}
		} else {
			d.c += uint64(next) << 9
			d.ct = 7
		}
	} else {
		b, err = d.reader.ReadByte()
		if err != nil {
			return err
		}
		d.currentByte = b
		d.c += uint64(d.currentByte) << 8
		d.ct = 8
	}
	d.c &= 0xFFFFFFFFFF
	return nil
}

type DecoderStats struct {
	index       int32
	contextSize int32
	contexts    []byte
	mps         []byte
}

func NewStats(contextSize int32, index int32) *DecoderStats {
	return &DecoderStats{index: index, contextSize: contextSize, contexts: make([]byte, contextSize), mps: make([]byte, contextSize)}
}
func (s *DecoderStats) Reset() {
	for i := 0; i < len(s.contexts); i++ {
		s.contexts[i] = 0
		s.mps[i] = 0
	}
}
func (d *Decoder) renormalize() error {
	for {
		if d.ct == 0 {
			if err := d.readByte(); err != nil {
				return err
			}
		}
		d.a <<= 1
		d.c <<= 1
		d.ct--
		if (d.a & 0x8000) != 0 {
			break
		}
	}
	d.c &= 0xffffffff
	return nil
}
func (s *DecoderStats) setEntry(entry int) { v := byte(entry & 0x7f); s.contexts[s.index] = v }
