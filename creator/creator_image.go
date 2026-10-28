package creator

type imageMaskKind int

const (
	imageMaskSoft imageMaskKind = iota + 1
	imageMaskStencil
	imageMaskColorKey
	imageMaskOptimizedAlpha
)
