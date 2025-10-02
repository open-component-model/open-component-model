// Package tree provides functionality to render tree structures
package tree

import "github.com/jedib0t/go-pretty/v6/table"

// TreeStyle defines the Unicode characters used for rendering tree structures.
type TreeStyle struct {
	// CharItemVertical is the character used for vertical lines (│)
	CharItemVertical string
	// CharItemMiddle is the character used for middle branch connectors (├─)
	CharItemMiddle string
	// CharItemBottom is the character used for bottom branch connectors (└─)
	CharItemBottom string
	// CharChildIndicator is the character used to indicate nodes with children (●)
	CharChildIndicator string
}

var DefaultTreeStyle = TreeStyle{
	CharItemVertical:   "│  ",
	CharItemMiddle:     "├─",
	CharItemBottom:     "└─",
	CharChildIndicator: " ●",
}

func defaultTableStyle() table.Style {
	style := table.StyleDefault
	style.Options = table.OptionsNoBordersAndSeparators
	style.Box.Right = ""
	return style
}
