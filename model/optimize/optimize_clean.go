package optimize

import (
	"bytes"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/unitype"
)

// CleanFonts cleans up embedded fonts, reducing font sizes.
type CleanFonts struct {
	// Subset embedded fonts if encountered (if true).
	// Otherwise attempts to reduce the font program.
	Subset bool
}

func cleanContentStream(stream *core.PdfObjectStream) error {
	data, err := core.DecodeStream(stream)
	if err != nil {
		return err
	}
	parser := contentstream.NewContentStreamParser(string(data))
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	ops = cleanOperations(ops)
	cleaned := ops.Bytes()
	if len(cleaned) >= len(data) {
		return nil
	}
	newStream, err := core.MakeStream(ops.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	stream.Stream = newStream.Stream
	stream.Merge(newStream.PdfObjectDictionary)
	return nil
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CleanFonts) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	var subsetted map[*core.PdfObjectStream]struct{}
	if o.Subset {
		var subsetErr error
		subsetted, subsetErr = subsetFonts(objects)
		if subsetErr != nil {
			common.Log.Debug("ERROR: Failed subsetting: %v", subsetErr)
			return nil, subsetErr
		}
	}
	for _, obj := range objects {
		stream, ok := core.GetStream(obj)
		if !ok {
			continue
		}
		if _, isSubsetted := subsetted[stream]; isSubsetted {
			continue
		}
		encoder, err := core.NewEncoderFromStream(stream)
		if err != nil {
			common.Log.Debug("ERROR getting encoder: %v - ignoring", err)
			continue
		}
		data, err := encoder.DecodeStream(stream)
		if err != nil {
			common.Log.Debug("Decoding error : %v - ignoring", err)
			continue
		}
		if len(data) < 4 {
			continue
		}
		magic := string(data[:4])
		if magic == "OTTO" {
			continue
		}
		if magic != "\x00\x01\x00\x00" && magic != "true" {
			continue
		}
		font, err := unitype.Parse(bytes.NewReader(data))
		if err != nil {
			common.Log.Debug("ERROR Parsing font: %v - ignoring", err)
			continue
		}
		err = font.Optimize()
		if err != nil {
			common.Log.Debug("ERROR Optimizing font: %v - skipping", err)
			continue
		}
		var buf bytes.Buffer
		err = font.Write(&buf)
		if err != nil {
			common.Log.Debug("ERROR Writing font: %v - ignoring", err)
			continue
		}
		if buf.Len() > len(data) {
			common.Log.Debug("Re-written font is larger than original - skip")
			continue
		}
		newStream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
		if err != nil {
			continue
		}
		*stream = *newStream
		stream.Set("Length1", core.MakeInteger(int64(buf.Len())))
	}
	return objects, nil
}

// CleanContentstream cleans up redundant operands in content streams, including Page and XObject Form
// contents. This process includes:
// 1. Marked content operators are removed.
// 2. Some operands are simplified (shorter form).
// TODO: Add more reduction methods and improving the methods for identifying unnecessary operands.
type CleanContentstream struct{}

func cleanOperations(ops *contentstream.ContentStreamOperations) *contentstream.ContentStreamOperations {
	if ops == nil {
		return nil
	}
	result := contentstream.ContentStreamOperations{}
	for _, op := range *ops {
		switch op.Operand {
		case "BDC", "BMC", "EMC":
			continue
		case "Tm":
			if len(op.Params) == 6 {
				if nums, err := core.GetNumbersAsFloat(op.Params); err == nil {
					if nums[0] == 1 && nums[1] == 0 && nums[2] == 0 && nums[3] == 1 {
						op = &contentstream.ContentStreamOperation{Params: []core.PdfObject{op.Params[4], op.Params[5]}, Operand: "Td"}
					}
				}
			}
		}
		result = append(result, op)
	}
	return &result
}

// Optimize implements Optimizer interface.
func (o *CleanUnusedResources) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	unused, err := collectUnusedResources(objects)
	if err != nil {
		return nil, err
	}
	kept := []core.PdfObject{}
	for _, obj := range objects {
		_, isUnused := unused[obj]
		if isUnused {
			continue
		}
		kept = append(kept, obj)
	}
	return kept, nil
}

// Optimize optimizes PDF objects to decrease PDF size.
func (o *CleanContentstream) Optimize(objects []core.PdfObject) (result []core.PdfObject, err error) {
	seen := map[*core.PdfObjectStream]struct{}{}
	var streams []*core.PdfObjectStream
	add := func(stream *core.PdfObjectStream) {
		if _, exists := seen[stream]; !exists {
			seen[stream] = struct{}{}
			streams = append(streams, stream)
		}
	}
	removed := map[core.PdfObject]bool{}
	added := map[core.PdfObject]bool{}
	for _, obj := range objects {
		switch typed := obj.(type) {
		case *core.PdfIndirectObject:
			switch inner := typed.PdfObject.(type) {
			case *core.PdfObjectDictionary:
				if typeName, ok := core.GetName(inner.Get("Type")); !ok || typeName.String() != "Page" {
					continue
				}
				if contents, ok := core.GetStream(inner.Get("Contents")); ok {
					add(contents)
				} else if arr, ok := core.GetArray(inner.Get("Contents")); ok {
					var parts []*core.PdfObjectStream
					for _, elem := range arr.Elements() {
						if s, ok := core.GetStream(elem); ok {
							parts = append(parts, s)
						}
					}
					if len(parts) > 0 {
						var buf bytes.Buffer
						for _, part := range parts {
							if data, err := core.DecodeStream(part); err == nil {
								buf.Write(data)
							}
							removed[part] = true
						}
						merged, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
						if err != nil {
							return nil, err
						}
						added[merged] = true
						inner.Set("Contents", merged)
						add(merged)
					}
				}
			}
		case *core.PdfObjectStream:
			if typeName, ok := core.GetName(typed.Get("Type")); !ok || typeName.String() != "XObject" {
				continue
			}
			if subtypeName, ok := core.GetName(typed.Get("Subtype")); !ok || subtypeName.String() != "Form" {
				continue
			}
			add(typed)
		}
	}
	for _, stream := range streams {
		err = cleanContentStream(stream)
		if err != nil {
			return nil, err
		}
	}
	result = nil
	for _, obj := range objects {
		if removed[obj] {
			continue
		}
		result = append(result, obj)
	}
	for obj := range added {
		result = append(result, obj)
	}
	return result, nil
}

// CleanUnusedResources represents an optimizer used to clean unused resources.
type CleanUnusedResources struct{}
