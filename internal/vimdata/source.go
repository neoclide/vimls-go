package vimdata

const (
	// VimSourceTag and VimSourceCommit identify the current Vim baseline.
	VimSourceTag    = "v9.2.1235"
	VimSourceCommit = "12d91ed7bb9c7276216670aed8e4fee264a13703"

	// NeovimSourceCommit remains independent from the Vim language baseline.
	NeovimSourceCommit = "73923b0dd85bb936ba2f63ee916dabaa0603340d"

	// ManualVimTag and ManualVimCommit identify the separately reviewed source
	// for hand-maintained Vim metadata. Keep these distinct from the active
	// source so a Vim baseline update requires an explicit metadata review.
	ManualVimTag    = "v9.2.1235"
	ManualVimCommit = "12d91ed7bb9c7276216670aed8e4fee264a13703"
)
