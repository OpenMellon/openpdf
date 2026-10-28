package model

import (
	_ "image/gif"
)

// BorderStyle defines border type, typically used for annotations.
type BorderStyle int

// GetPdfName returns the PDF name used to indicate the border style.
// (Table 166 p. 395).
func (b *BorderStyle) GetPdfName() string {
	switch *b {
	case BorderStyleSolid:
		return "S"
	case BorderStyleDashed:
		return "D"
	case BorderStyleBeveled:
		return "B"
	case BorderStyleInset:
		return "I"
	case BorderStyleUnderline:
		return "U"
	}
	return ""
}

// NewBorderStyle returns an initialized PdfBorderStyle.
func NewBorderStyle() *PdfBorderStyle { borderStyle := &PdfBorderStyle{}; return borderStyle }

const (
	BorderStyleSolid     BorderStyle = iota
	BorderStyleDashed    BorderStyle = iota
	BorderStyleBeveled   BorderStyle = iota
	BorderStyleInset     BorderStyle = iota
	BorderStyleUnderline BorderStyle = iota
)

// PdfBorderEffect represents a PDF border effect.
type PdfBorderEffect struct {
	S *BorderEffect
	I *float64
}
