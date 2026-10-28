package contentstream

import (
	"bytes"

	"github.com/OpenMellon/openpdf/core"
)

func (ops *ContentStreamOperations) isWrapped() bool {
	if len(*ops) < 2 {
		return false
	}
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		default:
			if n < 1 {
				return false
			}
		}
	}
	return n == 0
}

// HasUnclosedQ checks if all the `q` operator is properly closed by `Q` operator.
func (ops *ContentStreamOperations) HasUnclosedQ() bool {
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		}
	}
	return n != 0
}

// ContentStreamOperations is a slice of ContentStreamOperations.
type ContentStreamOperations []*ContentStreamOperation

// ContentStreamOperation represents an operation in PDF contentstream which consists of
// an operand and parameters.
type ContentStreamOperation struct {
	Params  []core.PdfObject
	Operand string
}

// Bytes converts a set of content stream operations to a content stream byte presentation,
// i.e. the kind that can be stored as a PDF stream or string format.
func (ops *ContentStreamOperations) Bytes() []byte {
	var buf bytes.Buffer
	for _, op := range *ops {
		if op == nil {
			continue
		}
		if op.Operand == "BI" {
			buf.WriteString(op.Operand + "\n")
			buf.Write(op.Params[0].Write())
		} else {
			for _, obj := range op.Params {
				buf.Write(obj.Write())
				buf.WriteString(" ")
			}
			buf.WriteString(op.Operand + "\n")
		}
	}
	return buf.Bytes()
}

// WrapIfNeeded wraps the entire contents within q ... Q.  If unbalanced, then adds extra Qs at the end.
// Only does if needed. Ensures that when adding new content, one start with all states
// in the default condition.
func (ops *ContentStreamOperations) WrapIfNeeded() *ContentStreamOperations {
	if len(*ops) == 0 {
		return ops
	}
	if ops.isWrapped() {
		return ops
	}
	*ops = append([]*ContentStreamOperation{{Operand: "q"}}, *ops...)
	n := 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			n++
		case "Q":
			n--
		}
	}
	for n > 0 {
		*ops = append(*ops, &ContentStreamOperation{Operand: "Q"})
		n--
	}
	return ops
}

// String returns `ops.Bytes()` as a string.
func (ops *ContentStreamOperations) String() string { return string(ops.Bytes()) }
