package resourceview

type Action string

const (
	ActionNone     Action = ""
	ActionDetails  Action = "details"
	ActionFilter   Action = "filter"
	ActionCreate   Action = "create"
	ActionEdit     Action = "edit"
	ActionDelete   Action = "delete"
	ActionPublish  Action = "publish"
	ActionCommands Action = "commands"
)

// FooterActions controls the optional resource actions rendered and accepted
// by the shared table. Column selection and sorting remain available on every
// resource page.
type FooterActions struct {
	Create  bool
	Edit    bool
	Delete  bool
	Publish bool
	Filter  bool
	Mark    bool
}

type HitKind int

const (
	HitNone HitKind = iota
	HitRow
	HitHeader
	HitFooterFilter
	HitFooterAction
)

type Hit struct {
	Kind        HitKind
	Action      Action
	RowIndex    int
	ColumnIndex int
	X           int
	Y           int
}

type rect struct {
	x, y, width, height int
}

func (region rect) contains(x, y int) bool {
	return x >= region.x && x < region.x+region.width && y >= region.y && y < region.y+region.height
}

func (region rect) center() (int, int) {
	return region.x + region.width/2, region.y + region.height/2
}
