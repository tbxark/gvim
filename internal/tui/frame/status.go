package frame

// StatusItem is one plugin item as the layout sees it.
type StatusItem struct {
	ID, Plugin, Text string
	Right            bool
	Prio             int
	Clickable        bool
}
