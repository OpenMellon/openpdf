package creator

import (
	"encoding/xml"
	"strings"
)

type templateNode struct {
	value       any
	start       xml.StartElement
	parent      *templateNode
	children    []*templateNode
	childValues []any
	attrs       map[string]string
	textAttrs   map[string]string
	hasHandler  bool
	hasChild    bool
	line        int
	col         int
	offset      int64
	hasText     bool
}

func (tn *templateNode) activeParent() *templateNode {
	for parent := tn.parent; parent != nil; parent = parent.parent {
		if parent.hasHandler {
			return parent
		}
	}
	return nil
}

func (tn *templateNode) componentParent() *templateNode {
	for parent := tn.parent; parent != nil; parent = parent.parent {
		if parent.value != nil {
			return parent
		}
	}
	return nil
}

func (tn *templateNode) classNames() []string {
	for _, attr := range tn.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			return strings.Fields(attr.Value)
		}
	}
	return nil
}

func templateNodeValue(node *templateNode) any {
	if node == nil {
		return nil
	}
	return node.value
}
