package regress

func init() {
	scenarios = append(
		scenarios,
		scenario{name: "input-xref-table", input: "xref-table.pdf"},
		scenario{name: "input-incremental-update", input: "incremental-update.pdf"},
		scenario{name: "input-damaged-xref", input: "damaged-xref.pdf"},
		scenario{name: "input-xref-stream", input: "xref-stream.pdf"},
		scenario{name: "input-inline-image", input: "inline-image.pdf", pixels: true},
		scenario{name: "input-stream-filters", input: "stream-filters.pdf", pixels: true},
		// Page 1 inherits /Rotate 90 from the root Pages node, page 2 overrides it to 0 in an inner node, page 3
		// inherits 0 and has its own MediaBox.
		scenario{name: "input-page-tree", input: "page-tree.pdf", pixels: true},
		scenario{name: "input-acroform", input: "acroform.pdf"},
		scenario{name: "input-cid-identity-h", input: "cid-identity-h.pdf", pixels: true},
		scenario{name: "input-rc4-128", input: "rc4-128.pdf", password: "user", pixels: true},
		scenario{name: "input-aes-128", input: "aes-128.pdf", password: "user", pixels: true},
		scenario{name: "input-aes-256", input: "aes-256.pdf", password: "user", pixels: true},
	)
}
