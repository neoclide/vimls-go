package vimdata

const (
	// VimSourceTag and VimSourceCommit identify the current Vim baseline.
	VimSourceTag    = "v9.2.1132"
	VimSourceCommit = "f3dc0fee778439ac8ff8680b42552f7f13d47396"

	// NeovimSourceCommit remains independent from the Vim language baseline.
	NeovimSourceCommit = "73923b0dd85bb936ba2f63ee916dabaa0603340d"

	// ManualVimTag and ManualVimCommit identify the separately reviewed source
	// for hand-maintained Vim metadata. Keep these distinct from the active
	// source so a Vim baseline update requires an explicit metadata review.
	ManualVimTag    = "v9.2.1132"
	ManualVimCommit = "f3dc0fee778439ac8ff8680b42552f7f13d47396"
)
