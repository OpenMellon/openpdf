// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package mmr

import (
	"errors"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
)

func (dec *Decoder) fillBitmap(bm *bitmap.Bitmap, row int, offsets []int, count int) error {
	var cur byte
	x := 0
	byteIdx := bm.GetByteIndex(x, row)
	for i := 0; i < count; i++ {
		colour := byte(1)
		runEnd := offsets[i]
		if (i & 1) == 0 {
			colour = 0
		}
		for x < runEnd {
			cur = (cur << 1) | colour
			x++
			if (x & 7) == 0 {
				if err := bm.SetByte(byteIdx, cur); err != nil {
					return err
				}
				byteIdx++
				cur = 0
			}
		}
	}
	if (x & 7) != 0 {
		cur <<= uint(8 - (x & 7))
		if err := bm.SetByte(byteIdx, cur); err != nil {
			return err
		}
	}
	return nil
}

func (dec *Decoder) createLittleEndianTable(specs [][3]int) ([]*code, error) {
	table := make([]*code, firstLevelMask+1)
	for i := 0; i < len(specs); i++ {
		c := newCode(specs[i])
		if c.bitLen <= firstLevelBits {
			shift := firstLevelBits - c.bitLen
			base := c.bits << uint(shift)
			for j := (1 << uint(shift)) - 1; j >= 0; j-- {
				idx := base | j
				table[idx] = c
			}
		} else {
			prefix := c.bits >> uint(c.bitLen-firstLevelBits)
			if table[prefix] == nil {
				sub := newCode([3]int{})
				sub.subTable = make([]*code, secondLevelMask+1)
				table[prefix] = sub
			}
			if c.bitLen <= firstLevelBits+secondLevelBits {
				subShift := firstLevelBits + secondLevelBits - c.bitLen
				subBase := (c.bits << uint(subShift)) & secondLevelMask
				table[prefix].hasSubTable = true
				for j := (1 << uint(subShift)) - 1; j >= 0; j-- {
					table[prefix].subTable[subBase|j] = c
				}
			} else {
				return nil, errors.New("code table overflow in MMRDecoder")
			}
		}
	}
	return table, nil
}

const (
	modePass mmrCode = iota
	modeHorizontal
	modeVertical0
	modeVerticalR1
	modeVerticalR2
	modeVerticalR3
	modeVerticalL1
	modeVerticalL2
	modeVerticalL3
	modeExt1D
	modeExt2D
)

func (dec *Decoder) detectAndSkipEOL() error {
	for {
		c, err := dec.data.uncompressGetCode(dec.modeTable)
		if err != nil {
			return err
		}
		if c != nil && c.value == EOL {
			dec.data.bitPos += c.bitLen
		} else {
			return nil
		}
	}
}

type code struct {
	bitLen      int
	bits        int
	value       int
	subTable    []*code
	hasSubTable bool
}

var (
	modeCodes     = [][3]int{{4, 0x1, int(modePass)}, {3, 0x1, int(modeHorizontal)}, {1, 0x1, int(modeVertical0)}, {3, 0x3, int(modeVerticalR1)}, {6, 0x3, int(modeVerticalR2)}, {7, 0x3, int(modeVerticalR3)}, {3, 0x2, int(modeVerticalL1)}, {6, 0x2, int(modeVerticalL2)}, {7, 0x2, int(modeVerticalL3)}, {10, 0xf, int(modeExt1D)}, {12, 0xf, int(modeExt2D)}, {12, 0x1, int(EOL)}}
	whiteRunCodes = [][3]int{{4, 0x07, 2}, {4, 0x08, 3}, {4, 0x0B, 4}, {4, 0x0C, 5}, {4, 0x0E, 6}, {4, 0x0F, 7}, {5, 0x12, 128}, {5, 0x13, 8}, {5, 0x14, 9}, {5, 0x1B, 64}, {5, 0x07, 10}, {5, 0x08, 11}, {6, 0x17, 192}, {6, 0x18, 1664}, {6, 0x2A, 16}, {6, 0x2B, 17}, {6, 0x03, 13}, {6, 0x34, 14}, {6, 0x35, 15}, {6, 0x07, 1}, {6, 0x08, 12}, {7, 0x13, 26}, {7, 0x17, 21}, {7, 0x18, 28}, {7, 0x24, 27}, {7, 0x27, 18}, {7, 0x28, 24}, {7, 0x2B, 25}, {7, 0x03, 22}, {7, 0x37, 256}, {7, 0x04, 23}, {7, 0x08, 20}, {7, 0xC, 19}, {8, 0x12, 33}, {8, 0x13, 34}, {8, 0x14, 35}, {8, 0x15, 36}, {8, 0x16, 37}, {8, 0x17, 38}, {8, 0x1A, 31}, {8, 0x1B, 32}, {8, 0x02, 29}, {8, 0x24, 53}, {8, 0x25, 54}, {8, 0x28, 39}, {8, 0x29, 40}, {8, 0x2A, 41}, {8, 0x2B, 42}, {8, 0x2C, 43}, {8, 0x2D, 44}, {8, 0x03, 30}, {8, 0x32, 61}, {8, 0x33, 62}, {8, 0x34, 63}, {8, 0x35, 0}, {8, 0x36, 320}, {8, 0x37, 384}, {8, 0x04, 45}, {8, 0x4A, 59}, {8, 0x4B, 60}, {8, 0x5, 46}, {8, 0x52, 49}, {8, 0x53, 50}, {8, 0x54, 51}, {8, 0x55, 52}, {8, 0x58, 55}, {8, 0x59, 56}, {8, 0x5A, 57}, {8, 0x5B, 58}, {8, 0x64, 448}, {8, 0x65, 512}, {8, 0x67, 640}, {8, 0x68, 576}, {8, 0x0A, 47}, {8, 0x0B, 48}, {9, 0x01, zeroFill}, {9, 0x98, 1472}, {9, 0x99, 1536}, {9, 0x9A, 1600}, {9, 0x9B, 1728}, {9, 0xCC, 704}, {9, 0xCD, 768}, {9, 0xD2, 832}, {9, 0xD3, 896}, {9, 0xD4, 960}, {9, 0xD5, 1024}, {9, 0xD6, 1088}, {9, 0xD7, 1152}, {9, 0xD8, 1216}, {9, 0xD9, 1280}, {9, 0xDA, 1344}, {9, 0xDB, 1408}, {10, 0x01, zeroFill}, {11, 0x01, zeroFill}, {11, 0x08, 1792}, {11, 0x0C, 1856}, {11, 0x0D, 1920}, {12, 0x00, EOF}, {12, 0x01, EOL}, {12, 0x12, 1984}, {12, 0x13, 2048}, {12, 0x14, 2112}, {12, 0x15, 2176}, {12, 0x16, 2240}, {12, 0x17, 2304}, {12, 0x1C, 2368}, {12, 0x1D, 2432}, {12, 0x1E, 2496}, {12, 0x1F, 2560}}
	blackRunCodes = [][3]int{{2, 0x02, 3}, {2, 0x03, 2}, {3, 0x02, 1}, {3, 0x03, 4}, {4, 0x02, 6}, {4, 0x03, 5}, {5, 0x03, 7}, {6, 0x04, 9}, {6, 0x05, 8}, {7, 0x04, 10}, {7, 0x05, 11}, {7, 0x07, 12}, {8, 0x04, 13}, {8, 0x07, 14}, {9, 0x01, zeroFill}, {9, 0x18, 15}, {10, 0x01, zeroFill}, {10, 0x17, 16}, {10, 0x18, 17}, {10, 0x37, 0}, {10, 0x08, 18}, {10, 0x0F, 64}, {11, 0x01, zeroFill}, {11, 0x17, 24}, {11, 0x18, 25}, {11, 0x28, 23}, {11, 0x37, 22}, {11, 0x67, 19}, {11, 0x68, 20}, {11, 0x6C, 21}, {11, 0x08, 1792}, {11, 0x0C, 1856}, {11, 0x0D, 1920}, {12, 0x00, EOF}, {12, 0x01, EOL}, {12, 0x12, 1984}, {12, 0x13, 2048}, {12, 0x14, 2112}, {12, 0x15, 2176}, {12, 0x16, 2240}, {12, 0x17, 2304}, {12, 0x1C, 2368}, {12, 0x1D, 2432}, {12, 0x1E, 2496}, {12, 0x1F, 2560}, {12, 0x24, 52}, {12, 0x27, 55}, {12, 0x28, 56}, {12, 0x2B, 59}, {12, 0x2C, 60}, {12, 0x33, 320}, {12, 0x34, 384}, {12, 0x35, 448}, {12, 0x37, 53}, {12, 0x38, 54}, {12, 0x52, 50}, {12, 0x53, 51}, {12, 0x54, 44}, {12, 0x55, 45}, {12, 0x56, 46}, {12, 0x57, 47}, {12, 0x58, 57}, {12, 0x59, 58}, {12, 0x5A, 61}, {12, 0x5B, 256}, {12, 0x64, 48}, {12, 0x65, 49}, {12, 0x66, 62}, {12, 0x67, 63}, {12, 0x68, 30}, {12, 0x69, 31}, {12, 0x6A, 32}, {12, 0x6B, 33}, {12, 0x6C, 40}, {12, 0x6D, 41}, {12, 0xC8, 128}, {12, 0xC9, 192}, {12, 0xCA, 26}, {12, 0xCB, 27}, {12, 0xCC, 28}, {12, 0xCD, 29}, {12, 0xD2, 34}, {12, 0xD3, 35}, {12, 0xD4, 36}, {12, 0xD5, 37}, {12, 0xD6, 38}, {12, 0xD7, 39}, {12, 0xDA, 42}, {12, 0xDB, 43}, {13, 0x4A, 640}, {13, 0x4B, 704}, {13, 0x4C, 768}, {13, 0x4D, 832}, {13, 0x52, 1280}, {13, 0x53, 1344}, {13, 0x54, 1408}, {13, 0x55, 1472}, {13, 0x5A, 1536}, {13, 0x5B, 1600}, {13, 0x64, 1664}, {13, 0x65, 1728}, {13, 0x6C, 512}, {13, 0x6D, 576}, {13, 0x72, 896}, {13, 0x73, 960}, {13, 0x74, 1024}, {13, 0x75, 1088}, {13, 0x76, 1152}, {13, 0x77, 1216}}
)

const (
	EOF             = -3
	zeroFill        = -2
	EOL             = -1
	firstLevelBits  = 8
	firstLevelMask  = (1 << firstLevelBits) - 1
	secondLevelBits = 5
	secondLevelMask = (1 << secondLevelBits) - 1
)

func (dec *Decoder) UncompressMMR() (bm *bitmap.Bitmap, err error) {
	bm, err = bitmap.New(dec.width, dec.height)
	if err != nil {
		return nil, err
	}
	curOffsets := make([]int, bm.Width+5)
	refOffsets := make([]int, bm.Width+5)
	refOffsets[0] = bm.Width
	refCount := 1
	var curCount int
	for row := 0; row < bm.Height; row++ {
		curCount, err = dec.uncompress2d(dec.data, refOffsets, refCount, curOffsets, bm.Width)
		if err != nil {
			return nil, err
		}
		if curCount == EOF {
			break
		}
		if curCount > 0 {
			err = dec.fillBitmap(bm, row, curOffsets, curCount)
			if err != nil {
				return nil, err
			}
		}
		refOffsets, curOffsets = curOffsets, refOffsets
		refCount = curCount
	}
	if err = dec.detectAndSkipEOL(); err != nil {
		return nil, err
	}
	dec.data.align()
	return bm, nil
}

func (dec *Decoder) initTables() (err error) {
	if dec.whiteTable == nil {
		dec.whiteTable, err = dec.createLittleEndianTable(whiteRunCodes)
		if err != nil {
			return
		}
		dec.blackTable, err = dec.createLittleEndianTable(blackRunCodes)
		if err != nil {
			return
		}
		dec.modeTable, err = dec.createLittleEndianTable(modeCodes)
		if err != nil {
			return
		}
	}
	return nil
}

type (
	mmrCode int
	runData struct {
		reader     *bitwise.Reader
		bitPos     int
		lastBitPos int
		word       int
		buf        []byte
		bufOffset  int
		bufLen     int
	}
)

func New(r *bitwise.Reader, width, height int, dataOffset, dataLength int64) (*Decoder, error) {
	dec := &Decoder{width: width, height: height}
	sub, err := r.NewPartialReader(int(dataOffset), int(dataLength), false)
	if err != nil {
		return nil, err
	}
	data, err := newRunData(sub)
	if err != nil {
		return nil, err
	}
	_, err = r.Seek(sub.RelativePosition(), io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	dec.data = data
	if err := dec.initTables(); err != nil {
		return nil, err
	}
	return dec, nil
}

func minInt(a, b int) int {
	if a > b {
		return b
	}
	return a
}

type Decoder struct {
	width, height int
	data          *runData
	whiteTable    []*code
	blackTable    []*code
	modeTable     []*code
}

func newRunData(r *bitwise.Reader) (*runData, error) {
	rd := &runData{reader: r, bitPos: 0, lastBitPos: 1}
	size := minInt(maxInt(minBufferSize, int(r.Length())), maxBufferSize)
	rd.buf = make([]byte, size)
	if err := rd.fillBuffer(0); err != nil {
		if err == io.EOF {
			rd.buf = make([]byte, 10)
			common.Log.Debug("FillBuffer failed: %v", err)
		} else {
			return nil, err
		}
	}
	return rd, nil
}

func (rd *runData) uncompressGetCodeLittleEndian(table []*code) (*code, error) {
	bits, err := rd.uncompressGetNextCodeLittleEndian()
	if err != nil {
		common.Log.Debug("UncompressGetNextCodeLittleEndian failed: %v", err)
		return nil, err
	}
	bits &= 0xffffff
	idx := bits >> (codeWindowBits - firstLevelBits)
	c := table[idx]
	if c != nil && c.hasSubTable {
		idx = (bits >> (codeWindowBits - firstLevelBits - secondLevelBits)) & secondLevelMask
		c = c.subTable[idx]
	}
	return c, nil
}

const (
	maxBufferSize  int  = 1024 << 7
	minBufferSize  int  = 3
	codeWindowBits uint = 24
)

func newCode(spec [3]int) *code { return &code{bitLen: spec[0], bits: spec[1], value: spec[2]} }
func (rd *runData) uncompressGetNextCodeLittleEndian() (int, error) {
	shift := rd.bitPos - rd.lastBitPos
	if shift < 0 || shift > 24 {
		byteIdx := (rd.bitPos >> 3) - rd.bufOffset
		if byteIdx >= rd.bufLen {
			byteIdx += rd.bufOffset
			if err := rd.fillBuffer(byteIdx); err != nil {
				return 0, err
			}
			byteIdx -= rd.bufOffset
		}
		w := (uint32(rd.buf[byteIdx]&0xFF) << 16) | (uint32(rd.buf[byteIdx+1]&0xFF) << 8) | uint32(rd.buf[byteIdx+2]&0xFF)
		bitOff := uint32(rd.bitPos & 7)
		w <<= bitOff
		rd.word = int(w)
	} else {
		bitsLeft := rd.lastBitPos & 7
		avail := 7 - bitsLeft
		if shift <= avail {
			rd.word <<= uint(shift)
		} else {
			byteIdx := (rd.lastBitPos >> 3) + 3 - rd.bufOffset
			if byteIdx >= rd.bufLen {
				byteIdx += rd.bufOffset
				if err := rd.fillBuffer(byteIdx); err != nil {
					return 0, err
				}
				byteIdx -= rd.bufOffset
			}
			bitsLeft = 8 - bitsLeft
			for {
				rd.word <<= uint(bitsLeft)
				rd.word |= int(uint(rd.buf[byteIdx]) & 0xFF)
				shift -= bitsLeft
				byteIdx++
				bitsLeft = 8
				if !(shift >= 8) {
					break
				}
			}
			rd.word <<= uint(shift)
		}
	}
	rd.lastBitPos = rd.bitPos
	return rd.word, nil
}

func (c *code) String() string {
	return fmt.Sprintf("%d/%d/%d", c.bitLen, c.bits, c.value)
}

func (dec *Decoder) uncompress2d(rd *runData, ref []int, refCount int, cur []int, width int) (int, error) {
	var (
		refPos   int
		curCount int
		pos      int
		isWhite  = true
		err      error
		c        *code
	)
	ref[refCount] = width
	ref[refCount+1] = width
	ref[refCount+2] = width + 1
	ref[refCount+3] = width + 1
rowLoop:
	for pos < width {
		c, err = rd.uncompressGetCode(dec.modeTable)
		if err != nil {
			return EOL, nil
		}
		if c == nil {
			rd.bitPos++
			break rowLoop
		}
		rd.bitPos += c.bitLen
		switch mmrCode(c.value) {
		case modeVertical0:
			pos = ref[refPos]
		case modeVerticalR1:
			pos = ref[refPos] + 1
		case modeVerticalL1:
			pos = ref[refPos] - 1
		case modeHorizontal:
			for {
				var table []*code
				if isWhite {
					table = dec.whiteTable
				} else {
					table = dec.blackTable
				}
				c, err = rd.uncompressGetCode(table)
				if err != nil {
					return 0, err
				}
				if c == nil {
					break rowLoop
				}
				rd.bitPos += c.bitLen
				if c.value < 64 {
					if c.value < 0 {
						cur[curCount] = pos
						curCount++
						c = nil
						break rowLoop
					}
					pos += c.value
					cur[curCount] = pos
					curCount++
					break
				}
				pos += c.value
			}
			firstRunEnd := pos
		secondRunLoop:
			for {
				var table []*code
				if !isWhite {
					table = dec.whiteTable
				} else {
					table = dec.blackTable
				}
				c, err = rd.uncompressGetCode(table)
				if err != nil {
					return 0, err
				}
				if c == nil {
					break rowLoop
				}
				rd.bitPos += c.bitLen
				if c.value < 64 {
					if c.value < 0 {
						cur[curCount] = pos
						curCount++
						break rowLoop
					}
					pos += c.value
					if pos < width || pos != firstRunEnd {
						cur[curCount] = pos
						curCount++
					}
					break secondRunLoop
				}
				pos += c.value
			}
			for pos < width && ref[refPos] <= pos {
				refPos += 2
			}
			continue rowLoop
		case modePass:
			refPos++
			pos = ref[refPos]
			refPos++
			continue rowLoop
		case modeVerticalR2:
			pos = ref[refPos] + 2
		case modeVerticalL2:
			pos = ref[refPos] - 2
		case modeVerticalR3:
			pos = ref[refPos] + 3
		case modeVerticalL3:
			pos = ref[refPos] - 3
		default:
			if rd.bitPos == 12 && c.value == EOL {
				rd.bitPos = 0
				if _, err = dec.uncompress1d(rd, ref, width); err != nil {
					return 0, err
				}
				rd.bitPos++
				if _, err = dec.uncompress1d(rd, cur, width); err != nil {
					return 0, err
				}
				count, err := dec.uncompress1d(rd, ref, width)
				if err != nil {
					return EOF, err
				}
				rd.bitPos++
				return count, nil
			}
			pos = width
			continue rowLoop
		}
		if pos <= width {
			isWhite = !isWhite
			cur[curCount] = pos
			curCount++
			if refPos > 0 {
				refPos--
			} else {
				refPos++
			}
			for pos < width && ref[refPos] <= pos {
				refPos += 2
			}
		}
	}
	if cur[curCount] != width {
		cur[curCount] = width
	}
	if c == nil {
		return EOL, nil
	}
	return curCount, nil
}

func (rd *runData) fillBuffer(offset int) error {
	rd.bufOffset = offset
	_, err := rd.reader.Seek(int64(offset), io.SeekStart)
	if err != nil {
		if err == io.EOF {
			common.Log.Debug("Seak EOF")
			rd.bufLen = -1
		} else {
			return err
		}
	}
	if err == nil {
		rd.bufLen, err = rd.reader.Read(rd.buf)
		if err != nil {
			if err == io.EOF {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Read EOF")
				}
				rd.bufLen = -1
			} else {
				return err
			}
		}
	}
	if rd.bufLen > -1 && rd.bufLen < 3 {
		for rd.bufLen < 3 {
			b, err := rd.reader.ReadByte()
			if err != nil {
				if err == io.EOF {
					rd.buf[rd.bufLen] = 0
				} else {
					return err
				}
			} else {
				rd.buf[rd.bufLen] = b & 0xFF
			}
			rd.bufLen++
		}
	}
	rd.bufLen -= 3
	if rd.bufLen < 0 {
		rd.buf = make([]byte, len(rd.buf))
		rd.bufLen = len(rd.buf) - 3
	}
	return nil
}

func (dec *Decoder) uncompress1d(rd *runData, cur []int, width int) (int, error) {
	var (
		isWhite  = true
		pos      int
		c        *code
		curCount int
		err      error
	)
rowLoop:
	for pos < width {
	runLoop:
		for {
			if isWhite {
				c, err = rd.uncompressGetCode(dec.whiteTable)
				if err != nil {
					return 0, err
				}
			} else {
				c, err = rd.uncompressGetCode(dec.blackTable)
				if err != nil {
					return 0, err
				}
			}
			rd.bitPos += c.bitLen
			if c.value < 0 {
				break rowLoop
			}
			pos += c.value
			if c.value < 64 {
				isWhite = !isWhite
				cur[curCount] = pos
				curCount++
				break runLoop
			}
		}
	}
	if cur[curCount] != width {
		cur[curCount] = width
	}
	count := EOL
	if c != nil && c.value != EOL {
		count = curCount
	}
	return count, nil
}

func maxInt(a, b int) int {
	if a < b {
		return b
	}
	return a
}

func (rd *runData) uncompressGetCode(table []*code) (*code, error) {
	return rd.uncompressGetCodeLittleEndian(table)
}
func (rd *runData) align() { rd.bitPos = ((rd.bitPos + 7) >> 3) << 3 }
