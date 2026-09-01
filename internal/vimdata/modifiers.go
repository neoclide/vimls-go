package vimdata

const (
	ModifierVimTag    = "v9.2.1015"
	ModifierVimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

// Modifier is an Ex command modifier from Vim tag v9.2.1015 commit
// 5ab969f719bb09555e90e8dff8c94fc37bcbf2ae src/ex_docmd.c. The four Vim9
// aggregate members are parser-only.
type Modifier struct {
	Name       string
	MinLen     int
	Vim9Member bool
}

var modifiers = []Modifier{
	{"aboveleft", 3, false}, {"belowright", 3, false}, {"botright", 2, false}, {"browse", 3, false}, {"confirm", 4, false}, {"filter", 4, false}, {"hide", 3, false}, {"horizontal", 3, false}, {"keepalt", 5, false}, {"keepjumps", 5, false}, {"keepmarks", 3, false}, {"keeppatterns", 5, false}, {"leftabove", 5, false}, {"legacy", 3, false}, {"lockmarks", 3, false}, {"noautocmd", 3, false}, {"noswapfile", 3, false}, {"rightbelow", 6, false}, {"sandbox", 3, false}, {"silent", 3, false}, {"tab", 3, false}, {"topleft", 2, false}, {"unsilent", 3, false}, {"verbose", 4, false}, {"vertical", 4, false}, {"vim9cmd", 4, false},
	{"abstract", 3, true}, {"export", 6, true}, {"public", 3, true}, {"static", 4, true},
}

// Modifiers returns a caller-owned copy in source order.
func Modifiers() []Modifier { return append([]Modifier(nil), modifiers...) }
