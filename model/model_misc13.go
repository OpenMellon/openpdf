package model

import (
	"bytes"
	"fmt"
	"image/color"
	_ "image/gif"
	"io"
	"os"
	"path/filepath"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/lazystream"
)

func widgetMKProperties(widget *PdfAnnotationWidget) (col, col2 PdfColor, s string, n int) {
	if widget == nil || widget.MK == nil {
		return
	}
	dict, ok := core.GetDict(widget.MK)
	if !ok {
		return
	}
	if bc, ok := core.GetArray(dict.Get("BC")); ok {
		col = colorFromArray(bc)
	}
	if bg, ok := core.GetArray(dict.Get("BG")); ok {
		col2 = colorFromArray(bg)
	}
	if ca, ok := core.GetString(dict.Get("CA")); ok {
		s = ca.Decoded()
	}
	if r, ok := core.GetInt(dict.Get("R")); ok {
		n = int(*r)
	}
	return
}

func isWhitespaceGap(parser *core.PdfParser, n, count int64) bool {
	if count-n <= 0 || count-n > maxGapBytes {
		return false
	}
	bytesAt, err := parser.ReadBytesAt(n, count-n)
	if err != nil {
		return false
	}
	for _, b := range bytesAt {
		if !core.IsWhiteSpace(b) {
			return false
		}
	}
	return true
}

func newWidgetProperties(widget *PdfAnnotationWidget) WidgetProperties {
	var widgetProperties WidgetProperties
	widgetProperties.BorderColor, widgetProperties.BackgroundColor, widgetProperties.Caption, widgetProperties.Rotation = widgetMKProperties(widget)
	if widget == nil || widget.PdfAnnotation == nil {
		return widgetProperties
	}
	if rectArr, ok := core.GetArray(widget.Rect); ok {
		if rect, err := NewPdfRectangle(*rectArr); err == nil {
			widgetProperties.Rect = rect
		}
	}
	if name, ok := core.GetName(widget.AS); ok {
		widgetProperties.AppearanceState = string(*name)
	}
	return widgetProperties
}

type xrefStreamPlan struct {
	objNum  int64
	entries []xrefEntry
	size    int64
	prev    int64
	hasPrev bool
	root    *core.PdfIndirectObject
	info    *core.PdfIndirectObject
	encrypt *core.PdfIndirectObject
	id      *core.PdfObjectArray
}

type xrefEntry struct {
	objNum     int64
	entryType  byte
	offset     uint32
	genOrIndex uint16
}

// WatermarkImageOptions contains options for configuring the image watermark process.
type WatermarkImageOptions struct {
	Alpha               float64
	FitToWidth          bool
	PreserveAspectRatio bool
	Angle               float64
}

// WatermarkTextOptions contains options for configuring the text watermark process.
type WatermarkTextOptions struct {
	// opacity, 0.5 by default.
	Alpha float64

	// font size, 20 by default.
	FontSize float64

	// it is mandatory to set FontPath to the path of .ttf font file you would like to use for drawing.
	FontPath string

	// font color, black by default.
	FontColor color.Color

	// rotation angle in degrees, 0 by default.
	Angle float64
}

func writeLazyStream(w io.Writer, n int, stream *core.PdfObjectStream, crypter *core.PdfCrypt) error {
	if stream.TempFile == "" {
		return fmt.Errorf("lazy stream %d: already written (lazy streams are consumed by the first write)", n)
	}
	if crypter != nil {
		take, ok := lazystream.Take(crypter, stream)
		if !ok {
			return fmt.Errorf("lazy stream %d: not encrypted by this writer", n)
		}
		if take != nil {
			_, err := w.Write(take)
			return err
		}
	}
	clean := filepath.Clean(stream.TempFile)
	open, err := os.Open(clean)
	if err != nil {
		return fmt.Errorf("lazy stream %d: temp file %q is not available (lazy streams are consumed by the first write): %w", n, clean, err)
	}
	data := make([]byte, 64*1024)
	for {
		count, err := open.Read(data)
		if count > 0 {
			if _, err := w.Write(data[:count]); err != nil {
				open.Close()
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			open.Close()
			return fmt.Errorf("lazy stream %d: reading temp file %q: %w", n, clean, err)
		}
	}
	open.Close()
	return nil
}

const (
	XObjectTypeUndefined XObjectType = iota
	XObjectTypeImage
	XObjectTypeForm
	XObjectTypePS
	XObjectTypeUnknown
)

// XObjectType represents the type of an XObject.
type XObjectType int

// WidgetProperties describes appearance characteristics for a single
// widget annotation of a form field.
type WidgetProperties struct {
	Rect            *PdfRectangle
	BorderColor     PdfColor
	BackgroundColor PdfColor
	Caption         string
	Rotation        int
	AppearanceState string
}

func writeTrailerDict(buf *bytes.Buffer, writer *PdfWriter, n int64, count int64, ok bool) {
	buf.WriteString("<<")
	fmt.Fprintf(buf, "/Size %d", n)
	if writer.root != nil {
		fmt.Fprintf(buf, "/Root %d 0 R", writer.root.ObjectNumber)
	}
	if writer.infoObj != nil {
		fmt.Fprintf(buf, "/Info %d 0 R", writer.infoObj.ObjectNumber)
	}
	if ok {
		fmt.Fprintf(buf, "/Prev %010d", count)
	}
	if writer.crypter != nil && writer.encryptObj != nil {
		fmt.Fprintf(buf, "/Encrypt %d 0 R", writer.encryptObj.ObjectNumber)
	}
	if writer.ids == nil && writer.id0 != "" && writer.id1 != "" {
		writer.ids = core.MakeArray(core.MakeHexString(writer.id0), core.MakeHexString(writer.id1))
	}
	if writer.ids != nil {
		buf.WriteString("/ID")
		buf.Write(writer.ids.Write())
	}
	buf.WriteString(">>")
}
